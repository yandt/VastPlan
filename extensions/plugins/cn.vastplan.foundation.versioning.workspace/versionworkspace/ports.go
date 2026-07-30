// Package versionworkspace implements the trusted, lease-bound Workspace
// Manager above Version Ledger and Resource Adapters.
package versionworkspace

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	versioningv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versioning/v1"
	resourcev1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionresource/v1"
)

const (
	PluginID      = "cn.vastplan.foundation.versioning.workspace"
	PluginVersion = "0.2.0"
	JSONAdapterID = "version.resource.json.v1"
)

type Scope struct {
	TenantID string
	ActorID  string
}

func (s Scope) Validate() error {
	if versioningv1.ValidateVersionIdentityTenant(s.TenantID) != nil || strings.TrimSpace(s.ActorID) == "" || len(s.ActorID) > 160 {
		return errors.New("Version Workspace tenant 或 actor 无效")
	}
	return nil
}

// Ledger is the narrow persistence port used by Workspace. Implementations
// translate transport errors before they cross this boundary.
type Ledger interface {
	PutVersion(context.Context, versioningv1.PutVersionRequest) (versioningv1.PutVersionResult, error)
	GetVersion(context.Context, versioningv1.GetVersionRequest) (versioningv1.GetVersionResult, error)
	GetHead(context.Context, versioningv1.GetHeadRequest) (versioningv1.GetHeadResult, error)
	CreateHead(context.Context, versioningv1.CreateHeadRequest) (versioningv1.CreateHeadResult, error)
	MoveHead(context.Context, versioningv1.MoveHeadRequest) (versioningv1.MoveHeadResult, error)
}

// Adapter is deliberately the same semantic surface as version.resource.v1.
// It may later be backed by a remote isolated runtime without changing Manager.
type Adapter interface {
	Descriptor() resourcev1.AdapterDescriptor
	Normalize(context.Context, resourcev1.AdapterNormalizeRequest) (resourcev1.AdapterNormalizeResult, error)
	Diff(context.Context, resourcev1.AdapterDiffRequest) (resourcev1.AdapterDiffResult, error)
}

type LedgerError struct {
	Code      string
	Retryable bool
	Err       error
}

func (e *LedgerError) Error() string {
	if e == nil || e.Err == nil {
		return "Version Ledger 调用失败"
	}
	return e.Err.Error()
}

func (e *LedgerError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func ledgerError(code string, retryable bool, err error) error {
	if err == nil {
		err = errors.New("Version Ledger 调用失败")
	}
	return &LedgerError{Code: code, Retryable: retryable, Err: err}
}

func ledgerContent(snapshot resourcev1.Snapshot, maxBytes int64) (json.RawMessage, error) {
	return resourcev1.CanonicalContent(snapshot, maxBytes)
}
