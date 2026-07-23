// Package sharedstate defines the versioned, caller-isolated persistence port
// available to trusted plugin runtimes. It contains no business state model.
package sharedstate

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	KernelServicePrefix = "kernel.state.shared."
	Protocol            = "state.shared.v1"
	MaxValueBytes = 1 << 20
	MaxPageSize   = 200
)

var (
	ErrNotFound = errors.New("shared state entry not found")
	ErrConflict = errors.New("shared state revision conflict")
	ErrInvalid  = errors.New("shared state request invalid")
)

type ScopeKind string

const (
	ScopeTenant  ScopeKind = "tenant"
	ScopeService ScopeKind = "service"
)

// Scope is created only by the trusted host. PluginID and RuntimeScope come
// from LaunchPolicy; TenantID comes from the authenticated CallContext.
type Scope struct {
	Kind         ScopeKind
	TenantID     string
	PluginID     string
	RuntimeScope string
	Namespace    string
}

func (s Scope) Validate() error {
	if s.Kind != ScopeTenant && s.Kind != ScopeService {
		return fmt.Errorf("%w: scope kind", ErrInvalid)
	}
	if !component(s.PluginID, 160) || !component(s.RuntimeScope, 256) || !namespace(s.Namespace) {
		return fmt.Errorf("%w: scope identity", ErrInvalid)
	}
	if s.Kind == ScopeTenant && !component(s.TenantID, 160) {
		return fmt.Errorf("%w: tenant scope", ErrInvalid)
	}
	if s.Kind == ScopeService && s.TenantID != "" {
		return fmt.Errorf("%w: service scope must not carry tenant", ErrInvalid)
	}
	return nil
}

type Entry struct {
	Key       string    `json:"key"`
	Value     []byte    `json:"value"`
	Revision  uint64    `json:"revision"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type Page struct {
	Items      []Entry `json:"items"`
	NextCursor string  `json:"nextCursor,omitempty"`
}

type Store interface {
	Get(context.Context, Scope, string) (Entry, error)
	Create(context.Context, Scope, string, []byte) (Entry, error)
	Update(context.Context, Scope, string, []byte, uint64) (Entry, error)
	Delete(context.Context, Scope, string, uint64) error
	List(context.Context, Scope, string, int, string) (Page, error)
}

func ValidateKey(key string) error {
	if !component(key, 320) || strings.ContainsAny(key, "\x00\r\n") {
		return fmt.Errorf("%w: key", ErrInvalid)
	}
	return nil
}

func ValidateValue(value []byte) error {
	if len(value) > MaxValueBytes {
		return fmt.Errorf("%w: value exceeds %d bytes", ErrInvalid, MaxValueBytes)
	}
	return nil
}

func ValidateList(prefix string, limit int, cursor string) error {
	if prefix != "" {
		if err := ValidateKey(prefix); err != nil {
			return err
		}
	}
	if limit < 1 || limit > MaxPageSize {
		return fmt.Errorf("%w: list limit", ErrInvalid)
	}
	if cursor != "" {
		if err := ValidateKey(cursor); err != nil {
			return fmt.Errorf("%w: list cursor", ErrInvalid)
		}
	}
	return nil
}

func component(value string, maximum int) bool {
	return value != "" && value == strings.TrimSpace(value) && utf8.ValidString(value) && utf8.RuneCountInString(value) <= maximum
}

func namespace(value string) bool {
	if !component(value, 120) {
		return false
	}
	for index, r := range value {
		if (r >= 'a' && r <= 'z') || (index > 0 && r >= '0' && r <= '9') || (index > 0 && (r == '.' || r == '-' || r == '_')) {
			continue
		}
		return false
	}
	return true
}
