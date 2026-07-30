// Package versionworkspacev1 defines the language-neutral, lease-bound
// editing session protocol layered above Version Ledger and Resource Adapters.
package versionworkspacev1

import (
	"time"

	versioningv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versioning/v1"
	versionresourcev1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionresource/v1"
)

const (
	SchemaURL  = "https://schemas.cdsoft.com.cn/vastplan/version-workspace/v1/vastplan.version-workspace.schema.json"
	Protocol   = "version.workspace.v1"
	Capability = "foundation.versioning.workspace"

	OperationOpen          = "open"
	OperationStatus        = "status"
	OperationReadSnapshot  = "readSnapshot"
	OperationWriteSnapshot = "writeSnapshot"
	OperationChanges       = "changes"
	OperationCommit        = "commit"
	OperationDiscard       = "discard"
	OperationRenew         = "renew"

	ErrorInvalidRequest      = "version.workspace.invalid_request"
	ErrorEnvironmentNotFound = "version.workspace.environment_not_found"
	ErrorResourceNotBound    = "version.workspace.resource_not_bound"
	ErrorSessionNotFound     = "version.workspace.session_not_found"
	ErrorSessionConflict     = "version.workspace.session_conflict"
	ErrorLeaseExpired        = "version.workspace.lease_expired"
	ErrorReadOnly            = "version.workspace.read_only"
	ErrorAdapterUnavailable  = "version.workspace.adapter_unavailable"
	ErrorLedgerUnavailable   = "version.workspace.ledger_unavailable"
	ErrorLimitExceeded       = "version.workspace.limit_exceeded"
	ErrorBaseConflict        = "version.workspace.base_conflict"

	StateClean      = "Clean"
	StateDirty      = "Dirty"
	StateCommitting = "Committing"
	StateCommitted  = "Committed"
	StateDiscarded  = "Discarded"
	StateExpired    = "Expired"

	MaxChangedPaths      = 10000
	MaxWireSnapshotBytes = 64 << 20
)

var knownErrorCodes = map[string]struct{}{
	ErrorInvalidRequest: {}, ErrorEnvironmentNotFound: {}, ErrorResourceNotBound: {},
	ErrorSessionNotFound: {}, ErrorSessionConflict: {}, ErrorLeaseExpired: {}, ErrorReadOnly: {},
	ErrorAdapterUnavailable: {}, ErrorLedgerUnavailable: {}, ErrorLimitExceeded: {}, ErrorBaseConflict: {},
}

func KnownErrorCode(code string) bool { _, ok := knownErrorCodes[code]; return ok }

type OpenRequest struct {
	EnvironmentID string                        `json:"environmentId"`
	Resource      versionresourcev1.ResourceKey `json:"resource"`
	RequestedMode string                        `json:"requestedMode,omitempty"`
	BaseRef       *versioningv1.VersionRef      `json:"baseRef,omitempty"`
	BaseHead      string                        `json:"baseHead,omitempty"`
	TargetHead    string                        `json:"targetHead,omitempty"`
	ReadOnly      bool                          `json:"readOnly"`
	LeaseSeconds  int                           `json:"leaseSeconds,omitempty"`
}

type Session struct {
	Protocol          string                        `json:"protocol"`
	ID                string                        `json:"id"`
	EnvironmentID     string                        `json:"environmentId"`
	EnvironmentDigest string                        `json:"environmentDigest"`
	Resource          versionresourcev1.ResourceKey `json:"resource"`
	Namespace         string                        `json:"namespace"`
	Adapter           string                        `json:"adapter"`
	Mode              string                        `json:"mode"`
	ReadOnly          bool                          `json:"readOnly"`
	BaseRef           *versioningv1.VersionRef      `json:"baseRef,omitempty"`
	BaseHead          string                        `json:"baseHead,omitempty"`
	TargetHead        string                        `json:"targetHead,omitempty"`
	HeadRevision      uint64                        `json:"headRevision,omitempty"`
	State             string                        `json:"state"`
	Revision          uint64                        `json:"revision"`
	CreatedAt         time.Time                     `json:"createdAt"`
	LeaseExpiresAt    time.Time                     `json:"leaseExpiresAt"`
}

type SessionRequest struct {
	SessionID string `json:"sessionId"`
}

type RevisionRequest struct {
	SessionID        string `json:"sessionId"`
	ExpectedRevision uint64 `json:"expectedRevision"`
}

type SnapshotResult struct {
	Session  Session                    `json:"session"`
	Snapshot versionresourcev1.Snapshot `json:"snapshot"`
	Digest   string                     `json:"digest"`
}

type WriteSnapshotRequest struct {
	SessionID        string                     `json:"sessionId"`
	ExpectedRevision uint64                     `json:"expectedRevision"`
	Snapshot         versionresourcev1.Snapshot `json:"snapshot"`
}

type ChangeSummary = versionresourcev1.ChangeSummary

type ChangesResult struct {
	Session      Session       `json:"session"`
	Dirty        bool          `json:"dirty"`
	ChangedPaths []string      `json:"changedPaths,omitempty"`
	Summary      ChangeSummary `json:"summary"`
}

type CommitRequest struct {
	SessionID        string            `json:"sessionId"`
	ExpectedRevision uint64            `json:"expectedRevision"`
	Message          string            `json:"message,omitempty"`
	Labels           map[string]string `json:"labels,omitempty"`
}

type CommitResult struct {
	Session Session                    `json:"session"`
	Version versioningv1.VersionRecord `json:"version"`
	Head    *versioningv1.Head         `json:"head,omitempty"`
}

type RenewRequest struct {
	SessionID        string `json:"sessionId"`
	ExpectedRevision uint64 `json:"expectedRevision"`
	LeaseSeconds     int    `json:"leaseSeconds"`
}

type SessionResult struct {
	Session Session `json:"session"`
}
