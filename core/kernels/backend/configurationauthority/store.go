// Package configurationauthority implements the cluster-wide, one-use
// ConfigurationAuthority store. Only hashes of bearer tokens are persisted.
package configurationauthority

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	sharedauthority "cdsoft.com.cn/VastPlan/core/shared/go/configurationauthority"
	sharedcontrolplane "cdsoft.com.cn/VastPlan/core/shared/go/controlplane"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfiguration"
)

const (
	stateIssued      = "Issued"
	stateConsumed    = "Consumed"
	maxIssueAttempts = 4
)

type record struct {
	TokenDigest string                 `json:"tokenDigest"`
	State       string                 `json:"state"`
	Claims      sharedauthority.Claims `json:"claims"`
	ConsumedAt  *time.Time             `json:"consumedAt,omitempty"`
}

type Store struct {
	KV       jetstream.KeyValue
	Catalogs pluginconfiguration.Reader
	Now      func() time.Time
	Random   io.Reader
}

func (s Store) Issue(ctx context.Context, tenant string, request sharedauthority.IssueRequest) (sharedauthority.Issued, error) {
	if s.KV == nil || s.Catalogs == nil || strings.TrimSpace(tenant) == "" || !validCandidateID(request.CandidateID) ||
		!strings.HasPrefix(request.ConfigurationID, "cfg_") || len(request.CatalogDigest) != 64 || strings.TrimSpace(request.FieldID) == "" ||
		(request.ResourceCollectionID == "") != (request.ResourceID == "") {
		return sharedauthority.Issued{}, sharedauthority.ErrInvalid
	}
	definition, err := s.findDefinition(ctx, tenant, request)
	if err != nil {
		return sharedauthority.Issued{}, err
	}
	fields := definition.ManagedCredentials
	schemaDigest := definition.SchemaDigest
	resourcePrefix := "plugin-configuration/" + definition.ID
	if request.ResourceCollectionID != "" {
		if !validResourceID(request.ResourceCollectionID, "cfgc_", 24) || !validResourceID(request.ResourceID, "cfgp_", 32) {
			return sharedauthority.Issued{}, sharedauthority.ErrInvalid
		}
		collection, ok := findResourceCollection(definition, request.ResourceCollectionID)
		if !ok {
			return sharedauthority.Issued{}, sharedauthority.ErrNotFound
		}
		fields, schemaDigest = collection.ManagedCredentials, collection.SchemaDigest
		resourcePrefix += "/" + collection.ID + "/" + request.ResourceID
	}
	purpose := ""
	for _, field := range fields {
		if field.ID == request.FieldID {
			purpose = field.Purpose
			break
		}
	}
	if purpose == "" {
		return sharedauthority.Issued{}, sharedauthority.ErrInvalid
	}
	now := s.now()
	for attempt := 0; attempt < maxIssueAttempts; attempt++ {
		token, digest, err := s.newToken()
		if err != nil {
			return sharedauthority.Issued{}, err
		}
		claims := sharedauthority.Claims{
			SchemaVersion: sharedauthority.SchemaVersion, AuthorityID: sharedauthority.TokenPrefix + digest,
			TenantID: tenant, ConfigurationID: definition.ID, ResourceCollectionID: request.ResourceCollectionID, ResourceID: request.ResourceID, CatalogDigest: request.CatalogDigest,
			Deployment: definition.Deployment, UnitID: definition.UnitID, CandidateID: request.CandidateID,
			FieldID: request.FieldID, Owner: definition.PluginID, Purpose: purpose,
			Resource:       resourcePrefix + "/" + request.CandidateID + "/" + request.FieldID,
			ArtifactSHA256: definition.Artifact.SHA256, SchemaDigest: schemaDigest,
			IssuedAt: now, ExpiresAt: now.Add(sharedauthority.DefaultTTL),
		}
		if err := claims.Validate(now, tenant); err != nil {
			return sharedauthority.Issued{}, err
		}
		raw, err := json.Marshal(record{TokenDigest: digest, State: stateIssued, Claims: claims})
		if err != nil {
			return sharedauthority.Issued{}, err
		}
		_, err = s.KV.Create(ctx, sharedcontrolplane.ConfigurationAuthorityKey(tenant, digest), raw)
		if err == nil {
			return sharedauthority.Issued{Token: token, ExpiresAt: claims.ExpiresAt}, nil
		}
		if !errors.Is(err, jetstream.ErrKeyExists) {
			return sharedauthority.Issued{}, fmt.Errorf("保存配置授权: %w", err)
		}
	}
	return sharedauthority.Issued{}, errors.New("生成配置授权时连续发生随机标识冲突")
}

func findResourceCollection(definition pluginconfiguration.Definition, id string) (pluginconfiguration.ResourceCollection, bool) {
	for _, collection := range definition.ResourceCollections {
		if collection.ID == id {
			return collection, true
		}
	}
	return pluginconfiguration.ResourceCollection{}, false
}

func validResourceID(value, prefix string, hexLength int) bool {
	if len(value) != len(prefix)+hexLength || !strings.HasPrefix(value, prefix) {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(value, prefix))
	return err == nil
}

func (s Store) Consume(ctx context.Context, tenant, token string) (sharedauthority.Claims, error) {
	if s.KV == nil || strings.TrimSpace(tenant) == "" || !validToken(token) {
		return sharedauthority.Claims{}, sharedauthority.ErrInvalid
	}
	digest := tokenDigest(token)
	key := sharedcontrolplane.ConfigurationAuthorityKey(tenant, digest)
	entry, err := s.KV.Get(ctx, key)
	if errors.Is(err, jetstream.ErrKeyNotFound) {
		return sharedauthority.Claims{}, sharedauthority.ErrNotFound
	}
	if err != nil {
		return sharedauthority.Claims{}, fmt.Errorf("读取配置授权: %w", err)
	}
	record, err := parseRecord(entry.Value())
	if err != nil || record.TokenDigest != digest || record.Claims.AuthorityID != sharedauthority.TokenPrefix+digest {
		return sharedauthority.Claims{}, sharedauthority.ErrInvalid
	}
	if record.State != stateIssued || record.ConsumedAt != nil {
		return sharedauthority.Claims{}, sharedauthority.ErrAlreadyConsumed
	}
	now := s.now()
	if err := record.Claims.Validate(now, tenant); err != nil {
		return sharedauthority.Claims{}, err
	}
	record.State, record.ConsumedAt = stateConsumed, &now
	raw, err := json.Marshal(record)
	if err != nil {
		return sharedauthority.Claims{}, err
	}
	if _, err := s.KV.Update(ctx, key, raw, entry.Revision()); err != nil {
		if errors.Is(err, jetstream.ErrKeyExists) || errors.Is(err, jetstream.ErrKeyNotFound) || strings.Contains(err.Error(), "wrong last sequence") {
			return sharedauthority.Claims{}, sharedauthority.ErrAlreadyConsumed
		}
		return sharedauthority.Claims{}, fmt.Errorf("消费配置授权: %w", err)
	}
	return record.Claims, nil
}

func (s Store) findDefinition(ctx context.Context, tenant string, request sharedauthority.IssueRequest) (pluginconfiguration.Definition, error) {
	catalogs, err := s.Catalogs.List(ctx, tenant)
	if err != nil {
		return pluginconfiguration.Definition{}, err
	}
	for _, catalog := range catalogs {
		if err := catalog.Validate(); err != nil {
			return pluginconfiguration.Definition{}, errors.New("活动配置目录校验失败")
		}
		if catalog.Digest != request.CatalogDigest {
			continue
		}
		for _, definition := range catalog.Items {
			if definition.ID == request.ConfigurationID {
				return definition, nil
			}
		}
	}
	return pluginconfiguration.Definition{}, sharedauthority.ErrNotFound
}

func (s Store) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (s Store) newToken() (string, string, error) {
	raw := make([]byte, 32)
	reader := s.Random
	if reader == nil {
		reader = rand.Reader
	}
	if _, err := io.ReadFull(reader, raw); err != nil {
		return "", "", err
	}
	token := sharedauthority.TokenPrefix + hex.EncodeToString(raw)
	return token, tokenDigest(token), nil
}

func validToken(token string) bool {
	if len(token) != len(sharedauthority.TokenPrefix)+64 || !strings.HasPrefix(token, sharedauthority.TokenPrefix) {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(token, sharedauthority.TokenPrefix))
	return err == nil
}

func validCandidateID(id string) bool {
	if len(id) != len("pcfg_")+32 || !strings.HasPrefix(id, "pcfg_") {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(id, "pcfg_"))
	return err == nil
}

func tokenDigest(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func parseRecord(raw []byte) (record, error) {
	var value record
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return value, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return value, sharedauthority.ErrInvalid
	}
	return value, nil
}
