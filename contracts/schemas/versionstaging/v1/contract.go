// Package versionstagingv1 defines the lease-bound control protocol for
// staging file bytes outside the JSON Capability Bus before version commit.
package versionstagingv1

import (
	"time"

	versionresourcev1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionresource/v1"
)

const (
	SchemaURL  = "https://schemas.cdsoft.com.cn/vastplan/version-staging/v1/vastplan.version-staging.schema.json"
	Protocol   = "version.staging.v1"
	Capability = "foundation.versioning.content-staging"

	OperationBeginUpload    = "beginUpload"
	OperationUploadStatus   = "uploadStatus"
	OperationRenewUpload    = "renewUpload"
	OperationCompleteUpload = "completeUpload"
	OperationAbortUpload    = "abortUpload"

	ErrorInvalidRequest       = "version.staging.invalid_request"
	ErrorLeaseNotFound        = "version.staging.lease_not_found"
	ErrorLeaseConflict        = "version.staging.lease_conflict"
	ErrorLeaseExpired         = "version.staging.lease_expired"
	ErrorLimitExceeded        = "version.staging.limit_exceeded"
	ErrorDataIncomplete       = "version.staging.data_incomplete"
	ErrorStorageUnavailable   = "version.staging.storage_unavailable"
	ErrorOperationUnsupported = "version.staging.operation_unsupported"

	StatePending   = "Pending"
	StateUploading = "Uploading"
	StateVerifying = "Verifying"
	StateReady     = "Ready"
	StateRejected  = "Rejected"
	StateAborted   = "Aborted"
	StateExpired   = "Expired"

	FailureDigestMismatch    = "digest_mismatch"
	FailureSizeMismatch      = "size_mismatch"
	FailureAdmissionRejected = "admission_rejected"

	MinimumLeaseSeconds      = 30
	MaximumLeaseSeconds      = 86400
	MaximumDeclaredFileBytes = int64(1 << 40)
)

var knownErrorCodes = map[string]struct{}{
	ErrorInvalidRequest: {}, ErrorLeaseNotFound: {}, ErrorLeaseConflict: {}, ErrorLeaseExpired: {},
	ErrorLimitExceeded: {}, ErrorDataIncomplete: {}, ErrorStorageUnavailable: {}, ErrorOperationUnsupported: {},
}

func KnownErrorCode(code string) bool { _, ok := knownErrorCodes[code]; return ok }

// BeginUploadRequest is created by a trusted Workspace after resolving the
// Environment and Resource binding. Tenant and actor come only from CallContext.
type BeginUploadRequest struct {
	SessionID               string                        `json:"sessionId"`
	ExpectedSessionRevision uint64                        `json:"expectedSessionRevision"`
	EnvironmentDigest       string                        `json:"environmentDigest"`
	Resource                versionresourcev1.ResourceKey `json:"resource"`
	Path                    string                        `json:"path"`
	MediaType               string                        `json:"mediaType"`
	ExpectedDigest          string                        `json:"expectedDigest"`
	ExpectedSize            int64                         `json:"expectedSize"`
	LeaseSeconds            int                           `json:"leaseSeconds"`
}

// UploadLease is a control-plane identity. The same authenticated caller uses
// its ID on the separate streaming data plane; no URL, token or Provider leaks.
type UploadLease struct {
	Protocol          string                        `json:"protocol"`
	ID                string                        `json:"id"`
	SessionID         string                        `json:"sessionId"`
	EnvironmentDigest string                        `json:"environmentDigest"`
	Resource          versionresourcev1.ResourceKey `json:"resource"`
	Path              string                        `json:"path"`
	MediaType         string                        `json:"mediaType"`
	ExpectedDigest    string                        `json:"expectedDigest"`
	ExpectedSize      int64                         `json:"expectedSize"`
	ReceivedSize      int64                         `json:"receivedSize"`
	State             string                        `json:"state"`
	Revision          uint64                        `json:"revision"`
	CreatedAt         time.Time                     `json:"createdAt"`
	UpdatedAt         time.Time                     `json:"updatedAt"`
	LeaseExpiresAt    time.Time                     `json:"leaseExpiresAt"`
}

type UploadStatusRequest struct {
	UploadID string `json:"uploadId"`
}

type UploadRevisionRequest struct {
	UploadID         string `json:"uploadId"`
	ExpectedRevision uint64 `json:"expectedRevision"`
}

type RenewUploadRequest struct {
	UploadID         string `json:"uploadId"`
	ExpectedRevision uint64 `json:"expectedRevision"`
	LeaseSeconds     int    `json:"leaseSeconds"`
}

// ContentDescriptor is returned only after digest, size and admission checks.
// Version history stores its values in FileEntry, never the temporary lease ID.
type ContentDescriptor struct {
	Digest    string `json:"digest"`
	Size      int64  `json:"size"`
	MediaType string `json:"mediaType"`
}

type UploadStatusResult struct {
	Upload      UploadLease        `json:"upload"`
	Content     *ContentDescriptor `json:"content,omitempty"`
	FailureCode string             `json:"failureCode,omitempty"`
}
