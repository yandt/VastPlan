// Package versionledger owns the trusted Version Ledger coordinator and its
// first-party in-process storage Provider SPI.
package versionledger

import (
	"context"
	"errors"

	versioningv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versioning/v1"
)

const (
	PluginID      = "cn.vastplan.foundation.versioning.ledger"
	PluginVersion = "0.1.0"
)

type Scope struct {
	TenantID string
}

func (s Scope) Validate() error {
	if s.TenantID == "" || len(s.TenantID) > 256 {
		return errors.New("Version Ledger tenant 无效")
	}
	return nil
}

// Provider owns the atomic storage boundary. Implementations assign sequence,
// version ID and creation time inside PutVersion; the coordinator never
// allocates durable identity outside a Provider transaction.
type Provider interface {
	Descriptor() versioningv1.ProviderDescriptor
	PutVersion(context.Context, Scope, versioningv1.ProviderPutVersionRequest) (versioningv1.PutVersionResult, error)
	GetVersion(context.Context, Scope, versioningv1.GetVersionRequest) (versioningv1.GetVersionResult, error)
	ListHistory(context.Context, Scope, versioningv1.ListHistoryRequest) (versioningv1.ListHistoryResult, error)
	GetHead(context.Context, Scope, versioningv1.GetHeadRequest) (versioningv1.GetHeadResult, error)
	MoveHead(context.Context, Scope, versioningv1.MoveHeadRequest) (versioningv1.MoveHeadResult, error)
}

type ProviderError struct {
	Code      string
	Retryable bool
	Err       error
}

func (e *ProviderError) Error() string {
	if e == nil || e.Err == nil {
		return "version provider error"
	}
	return e.Err.Error()
}

func (e *ProviderError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func providerError(code string, retryable bool, err error) error {
	if err == nil {
		err = errors.New("version provider operation failed")
	}
	if !versioningv1.KnownErrorCode(code) {
		code = versioningv1.ErrorCorrupted
	}
	return &ProviderError{Code: code, Retryable: retryable, Err: err}
}

func errorDetails(err error) (string, bool) {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return versioningv1.ErrorProviderUnavailable, true
	}
	var providerErr *ProviderError
	if errors.As(err, &providerErr) {
		return providerErr.Code, providerErr.Retryable
	}
	return versioningv1.ErrorProviderUnavailable, true
}
