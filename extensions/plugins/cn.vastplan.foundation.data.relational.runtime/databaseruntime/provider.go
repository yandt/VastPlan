// Package databaseruntime owns the first-party in-process Provider SPI used by
// the dedicated Database Runtime plugin. The Backend Kernel never imports it.
package databaseruntime

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"sync"

	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
)

const (
	PluginID      = "cn.vastplan.foundation.data.relational.runtime"
	PluginVersion = "0.1.0"
)

// CredentialMaterial exists only during MaterialSource.WithMaterial. Provider
// implementations must not retain the returned slice after the callback.
type CredentialMaterial interface{ Bytes() []byte }

// MaterialSource is retained by a Pool and invoked only when a physical
// connection needs credential material. It prevents Provider configuration
// from carrying passwords, DSNs or long-lived plaintext.
type MaterialSource interface {
	WithMaterial(context.Context, func(CredentialMaterial) error) error
}

type PoolStats struct {
	Open    int64
	Idle    int64
	InUse   int64
	Waiting int64
	MaxOpen int64
	Healthy bool
}

// Provider creates one local pool for one validated connection generation.
// Third-party providers will use a versioned RPC adapter rather than implement
// this Go interface across an ABI boundary.
type Provider interface {
	Descriptor() databasev1.ProviderDescriptor
	Validate(context.Context, databasev1.ConnectionSpec) error
	OpenPool(context.Context, databasev1.ConnectionSpec, MaterialSource) (Pool, error)
}

type Pool interface {
	Probe(context.Context) error
	Query(context.Context, databasev1.Statement, int) (databasev1.QueryResult, error)
	Execute(context.Context, databasev1.Statement) (databasev1.ExecuteResult, error)
	Begin(context.Context, databasev1.TransactionOptions) (Transaction, error)
	Stats() PoolStats
	Close() error
}

type Transaction interface {
	Query(context.Context, databasev1.Statement, int) (databasev1.QueryResult, error)
	Execute(context.Context, databasev1.Statement) (databasev1.ExecuteResult, error)
	Commit(context.Context) error
	Rollback(context.Context) error
}

// RuntimeError is the only Provider error shape interpreted by the service.
// Message remains diagnostic; callers branch only on the stable code.
type RuntimeError struct {
	Code      string
	Retryable bool
	Err       error
}

func (e *RuntimeError) Error() string {
	if e == nil || e.Err == nil {
		return "database runtime error"
	}
	return e.Err.Error()
}

func (e *RuntimeError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func NewRuntimeError(code string, retryable bool, err error) error {
	if err == nil {
		err = errors.New("database runtime operation failed")
	}
	if !databasev1.KnownErrorCode(code) {
		code = databasev1.ErrorQueryFailed
	}
	return &RuntimeError{Code: code, Retryable: retryable, Err: err}
}

func ErrorDetails(err error) (code string, retryable bool) {
	if errors.Is(err, context.DeadlineExceeded) {
		return databasev1.ErrorDeadlineExceeded, true
	}
	var runtimeErr *RuntimeError
	if errors.As(err, &runtimeErr) {
		return runtimeErr.Code, runtimeErr.Retryable
	}
	return databasev1.ErrorQueryFailed, false
}

type Registry struct {
	mu        sync.RWMutex
	providers map[string]registeredProvider
}

type registeredProvider struct {
	provider   Provider
	descriptor databasev1.ProviderDescriptor
}

func NewRegistry() *Registry { return &Registry{providers: map[string]registeredProvider{}} }

func (r *Registry) Register(provider Provider) error {
	if r == nil || nilInterface(provider) {
		return errors.New("Database Provider 不能为空")
	}
	descriptor := provider.Descriptor()
	if err := databasev1.ValidateProviderDescriptor(descriptor); err != nil {
		return fmt.Errorf("Database Provider descriptor: %w", err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.providers[descriptor.ID]; exists {
		return fmt.Errorf("Database Provider %q 重复注册", descriptor.ID)
	}
	descriptor.ConfigurationSchema = append([]byte(nil), descriptor.ConfigurationSchema...)
	r.providers[descriptor.ID] = registeredProvider{provider: provider, descriptor: descriptor}
	return nil
}

func nilInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

// Validate resolves the exact Provider and applies both the public wire rules
// and Provider-specific semantic validation. Callers should use this method
// rather than invoke Provider.Validate directly.
func (r *Registry) Validate(ctx context.Context, spec databasev1.ConnectionSpec) (Provider, error) {
	if ctx == nil {
		return nil, NewRuntimeError(databasev1.ErrorInvalidRequest, false, errors.New("context 不能为空"))
	}
	if err := databasev1.ValidateConnectionSpec(spec); err != nil {
		return nil, NewRuntimeError(databasev1.ErrorInvalidRequest, false, err)
	}
	provider, ok := r.Resolve(spec.ProviderID)
	if !ok {
		return nil, NewRuntimeError(databasev1.ErrorProviderNotFound, false,
			fmt.Errorf("Database Provider %q 未注册", spec.ProviderID))
	}
	if err := provider.Validate(ctx, spec); err != nil {
		return nil, NewRuntimeError(databasev1.ErrorInvalidRequest, false, err)
	}
	return provider, nil
}

func (r *Registry) OpenPool(ctx context.Context, spec databasev1.ConnectionSpec, material MaterialSource) (Pool, error) {
	if nilInterface(material) {
		return nil, NewRuntimeError(databasev1.ErrorInvalidRequest, false, errors.New("MaterialSource 不能为空"))
	}
	provider, err := r.Validate(ctx, spec)
	if err != nil {
		return nil, err
	}
	pool, err := provider.OpenPool(ctx, spec, material)
	if err != nil {
		var runtimeErr *RuntimeError
		if errors.As(err, &runtimeErr) {
			return nil, err
		}
		return nil, NewRuntimeError(databasev1.ErrorConnectionUnavailable, true, err)
	}
	if nilInterface(pool) {
		return nil, NewRuntimeError(databasev1.ErrorConnectionUnavailable, true,
			errors.New("Provider 返回空连接池"))
	}
	return pool, nil
}

func (r *Registry) Resolve(providerID string) (Provider, bool) {
	if r == nil {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	registered, ok := r.providers[providerID]
	return registered.provider, ok
}

func (r *Registry) Descriptors() []databasev1.ProviderDescriptor {
	if r == nil {
		return []databasev1.ProviderDescriptor{}
	}
	r.mu.RLock()
	descriptors := make([]databasev1.ProviderDescriptor, 0, len(r.providers))
	for _, registered := range r.providers {
		descriptor := registered.descriptor
		descriptor.ConfigurationSchema = append([]byte(nil), descriptor.ConfigurationSchema...)
		descriptors = append(descriptors, descriptor)
	}
	r.mu.RUnlock()
	sort.Slice(descriptors, func(i, j int) bool { return descriptors[i].ID < descriptors[j].ID })
	return descriptors
}
