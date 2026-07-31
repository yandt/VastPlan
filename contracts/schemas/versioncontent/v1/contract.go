// Package versioncontentv1 defines the durable hand-off protocol between a
// trusted Workspace transaction and content-addressed storage.
package versioncontentv1

import (
	"time"

	versioningv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versioning/v1"
	versionresourcev1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionresource/v1"
)

const (
	SchemaURL  = "https://schemas.cdsoft.com.cn/vastplan/version-content/v1/vastplan.version-content.schema.json"
	Protocol   = "version.content-reference.v1"
	Capability = "foundation.versioning.content-reference"

	OperationPrepare = "prepareVersion"
	OperationStatus  = "protectionStatus"
	OperationConfirm = "confirmVersion"
	OperationAbort   = "abortProtection"

	ErrorInvalidRequest     = "version.content.invalid_request"
	ErrorProtectionNotFound = "version.content.protection_not_found"
	ErrorConflict           = "version.content.conflict"
	ErrorContentUnavailable = "version.content.content_unavailable"
	ErrorLimitExceeded      = "version.content.limit_exceeded"
	ErrorStorageUnavailable = "version.content.storage_unavailable"
	ErrorUnsupported        = "version.content.unsupported"

	StatePrepared  = "Prepared"
	StateConfirmed = "Confirmed"
	StateAborted   = "Aborted"
	StateExpired   = "Expired"
)

type ContentEntry struct {
	Path      string `json:"path"`
	UploadID  string `json:"uploadId,omitempty"`
	Digest    string `json:"digest"`
	Size      int64  `json:"size"`
	MediaType string `json:"mediaType"`
}

// PrepareRequest creates a durable intent before Ledger.PutVersion. UploadID
// is required for new bytes and omitted only when a confirmed version already
// protects the exact content identity.
type PrepareRequest struct {
	OperationID             string                        `json:"operationId"`
	SessionID               string                        `json:"sessionId"`
	ExpectedSessionRevision uint64                        `json:"expectedSessionRevision"`
	EnvironmentDigest       string                        `json:"environmentDigest"`
	Resource                versionresourcev1.ResourceKey `json:"resource"`
	Stream                  versioningv1.StreamKey        `json:"stream"`
	ManifestDigest          string                        `json:"manifestDigest"`
	Entries                 []ContentEntry                `json:"entries"`
}

type StatusRequest struct {
	ProtectionID string `json:"protectionId"`
}

type ConfirmRequest struct {
	ProtectionID     string                  `json:"protectionId"`
	ExpectedRevision uint64                  `json:"expectedRevision"`
	Version          versioningv1.VersionRef `json:"version"`
}

type AbortRequest struct {
	ProtectionID     string `json:"protectionId"`
	ExpectedRevision uint64 `json:"expectedRevision"`
}

// Protection is the public state projection. Provider paths, temporary
// upload identities and physical object keys remain private to the host.
type Protection struct {
	Protocol          string                        `json:"protocol"`
	ID                string                        `json:"id"`
	OperationID       string                        `json:"operationId"`
	EnvironmentDigest string                        `json:"environmentDigest"`
	Resource          versionresourcev1.ResourceKey `json:"resource"`
	Stream            versioningv1.StreamKey        `json:"stream"`
	ManifestDigest    string                        `json:"manifestDigest"`
	Entries           []ContentEntry                `json:"entries"`
	State             string                        `json:"state"`
	Revision          uint64                        `json:"revision"`
	Version           *versioningv1.VersionRef      `json:"version,omitempty"`
	CreatedAt         time.Time                     `json:"createdAt"`
	UpdatedAt         time.Time                     `json:"updatedAt"`
	ExpiresAt         *time.Time                    `json:"expiresAt,omitempty"`
}

type ProtectionResult struct {
	Protection Protection `json:"protection"`
}

var knownErrorCodes = map[string]struct{}{
	ErrorInvalidRequest: {}, ErrorProtectionNotFound: {}, ErrorConflict: {},
	ErrorContentUnavailable: {}, ErrorLimitExceeded: {}, ErrorStorageUnavailable: {}, ErrorUnsupported: {},
}

func KnownErrorCode(code string) bool { _, ok := knownErrorCodes[code]; return ok }
