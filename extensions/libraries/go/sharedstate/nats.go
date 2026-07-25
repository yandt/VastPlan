package sharedstate

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/nats-io/nats.go/jetstream"
)

// NATSStore uses one externally managed JetStream KV bucket. It relies on KV
// revisions for cross-node CAS and never exposes physical bucket keys to a
// plugin response.
type NATSStore struct{ KV jetstream.KeyValue }

func NewNATSStore(kv jetstream.KeyValue) (*NATSStore, error) {
	if kv == nil {
		return nil, errors.New("shared state NATS KV 不能为空")
	}
	return &NATSStore{KV: kv}, nil
}

func (s *NATSStore) Get(ctx context.Context, scope Scope, key string) (Entry, error) {
	physical, err := physicalKey(scope, key)
	if err != nil {
		return Entry{}, err
	}
	value, err := s.KV.Get(ctx, physical)
	if err != nil {
		return Entry{}, readError(err)
	}
	return entry(key, value), nil
}

func (s *NATSStore) Create(ctx context.Context, scope Scope, key string, value []byte) (Entry, error) {
	physical, err := physicalKey(scope, key)
	if err != nil {
		return Entry{}, err
	}
	if err := ValidateValue(value); err != nil {
		return Entry{}, err
	}
	revision, err := s.KV.Create(ctx, physical, append([]byte(nil), value...))
	if err != nil {
		if errors.Is(err, jetstream.ErrKeyExists) {
			return Entry{}, ErrConflict
		}
		return Entry{}, err
	}
	return s.getRevision(ctx, key, physical, revision)
}

func (s *NATSStore) Update(ctx context.Context, scope Scope, key string, value []byte, expected uint64) (Entry, error) {
	physical, err := physicalKey(scope, key)
	if err != nil || expected == 0 {
		if err != nil {
			return Entry{}, err
		}
		return Entry{}, ErrInvalid
	}
	if err := ValidateValue(value); err != nil {
		return Entry{}, err
	}
	revision, err := s.KV.Update(ctx, physical, append([]byte(nil), value...), expected)
	if err != nil {
		if errors.Is(err, jetstream.ErrKeyExists) || errors.Is(err, jetstream.ErrKeyNotFound) {
			return Entry{}, ErrConflict
		}
		return Entry{}, err
	}
	return s.getRevision(ctx, key, physical, revision)
}

func (s *NATSStore) Delete(ctx context.Context, scope Scope, key string, expected uint64) error {
	physical, err := physicalKey(scope, key)
	if err != nil || expected == 0 {
		if err != nil {
			return err
		}
		return ErrInvalid
	}
	if err := s.KV.Delete(ctx, physical, jetstream.LastRevision(expected)); err != nil {
		if errors.Is(err, jetstream.ErrKeyExists) {
			return ErrConflict
		}
		if errors.Is(err, jetstream.ErrKeyNotFound) {
			return ErrNotFound
		}
		return err
	}
	return nil
}

func (s *NATSStore) List(ctx context.Context, scope Scope, prefix string, limit int, cursor string) (Page, error) {
	if err := scope.Validate(); err != nil {
		return Page{}, err
	}
	if err := ValidateList(prefix, limit, cursor); err != nil {
		return Page{}, err
	}
	physicalPrefix := scopePrefix(scope)
	lister, err := s.KV.ListKeysFiltered(ctx, physicalPrefix+">")
	if err != nil && !errors.Is(err, jetstream.ErrNoKeysFound) {
		return Page{}, err
	}
	if lister == nil {
		return Page{Items: []Entry{}}, nil
	}
	defer lister.Stop()
	logical := make([]string, 0)
	for key := range lister.Keys() {
		if !strings.HasPrefix(key, physicalPrefix) {
			continue
		}
		decoded, decodeErr := decode(strings.TrimPrefix(key, physicalPrefix))
		if decodeErr == nil && strings.HasPrefix(decoded, prefix) && decoded > cursor {
			logical = append(logical, decoded)
		}
	}
	sort.Strings(logical)
	page := Page{Items: []Entry{}}
	for _, key := range logical {
		if len(page.Items) == limit {
			page.NextCursor = page.Items[len(page.Items)-1].Key
			break
		}
		item, getErr := s.Get(ctx, scope, key)
		if errors.Is(getErr, ErrNotFound) {
			continue
		}
		if getErr != nil {
			return Page{}, getErr
		}
		page.Items = append(page.Items, item)
	}
	return page, nil
}

func (s *NATSStore) getRevision(ctx context.Context, logical, physical string, revision uint64) (Entry, error) {
	value, err := s.KV.GetRevision(ctx, physical, revision)
	if err != nil {
		return Entry{}, err
	}
	return entry(logical, value), nil
}

func entry(key string, value jetstream.KeyValueEntry) Entry {
	return Entry{Key: key, Value: append([]byte(nil), value.Value()...), Revision: value.Revision(), UpdatedAt: value.Created().UTC()}
}

func readError(err error) error {
	if errors.Is(err, jetstream.ErrKeyNotFound) {
		return ErrNotFound
	}
	return err
}

func physicalKey(scope Scope, key string) (string, error) {
	if err := scope.Validate(); err != nil {
		return "", err
	}
	if err := ValidateKey(key); err != nil {
		return "", err
	}
	return scopePrefix(scope) + encode(key), nil
}

// ParsePhysicalKeyForOperations decodes a physical Shared State key for
// trusted backup, recovery, and capacity tooling. Plugin-facing protocols must
// never expose this representation because it contains host-derived identity.
func ParsePhysicalKeyForOperations(key string) (Scope, string, error) {
	parts := strings.Split(key, ".")
	if len(parts) != 7 || parts[0] != "v1" {
		return Scope{}, "", fmt.Errorf("%w: physical key", ErrInvalid)
	}
	kind := ScopeKind(parts[1])
	tenant := ""
	if kind == ScopeTenant {
		var err error
		tenant, err = decode(parts[2])
		if err != nil {
			return Scope{}, "", fmt.Errorf("%w: physical tenant", ErrInvalid)
		}
	} else if kind != ScopeService || parts[2] != "-" {
		return Scope{}, "", fmt.Errorf("%w: physical scope", ErrInvalid)
	}
	decoded := make([]string, 4)
	for index, part := range parts[3:] {
		value, err := decode(part)
		if err != nil {
			return Scope{}, "", fmt.Errorf("%w: physical component", ErrInvalid)
		}
		decoded[index] = value
	}
	scope := Scope{Kind: kind, TenantID: tenant, RuntimeScope: decoded[0], PluginID: decoded[1], Namespace: decoded[2]}
	if err := scope.Validate(); err != nil {
		return Scope{}, "", err
	}
	if err := ValidateKey(decoded[3]); err != nil {
		return Scope{}, "", err
	}
	return scope, decoded[3], nil
}

func scopePrefix(scope Scope) string {
	tenant := "-"
	if scope.Kind == ScopeTenant {
		tenant = encode(scope.TenantID)
	}
	return "v1." + string(scope.Kind) + "." + tenant + "." + encode(scope.RuntimeScope) + "." + encode(scope.PluginID) + "." + encode(scope.Namespace) + "."
}

func encode(value string) string { return base64.RawURLEncoding.EncodeToString([]byte(value)) }
func decode(value string) (string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(value)
	return string(raw), err
}
