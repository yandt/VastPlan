// Package versionresourcev1 defines framework-neutral versioned resource
// descriptors shared by Workspace Managers and language-specific adapters.
package versionresourcev1

import (
	"encoding/json"
)

const (
	SchemaURL         = "https://schemas.cdsoft.com.cn/vastplan/version-resource/v1/vastplan.version-resource.schema.json"
	Protocol          = "version.resource.v1"
	AdapterCapability = "foundation.versioning.resource-adapter"

	AdapterOperationDescribe    = "describe"
	AdapterOperationNormalize   = "normalize"
	AdapterOperationDiff        = "diff"
	AdapterOperationMaterialize = "materialize"

	ModeSnapshot = "snapshot"
	ModeOverlay  = "overlay"
	ModeGit      = "git"

	ContentJSON            = "json"
	ContentFiles           = "files"
	FilesManifestMediaType = "application/vnd.vastplan.files+json"

	SecretPolicyForbidden          = "forbidden"
	SecretPolicyCredentialRefsOnly = "credential-refs-only"

	ProjectionNone        = "none"
	ProjectionDomainHot   = "domain-hot"
	ProjectionCurrentOnly = "current-only"

	MaxFileEntries  = 10000
	MaxChangedPaths = 10000
)

type ResourceKey struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type FileEntry struct {
	Path      string `json:"path"`
	Digest    string `json:"digest"`
	Size      int64  `json:"size"`
	Mode      uint32 `json:"mode"`
	MediaType string `json:"mediaType"`
}

// Snapshot carries either canonical JSON or a content-addressed file manifest.
// File bytes remain in the Artifact/Object store and never enter this object.
type Snapshot struct {
	Kind      string          `json:"kind"`
	MediaType string          `json:"mediaType"`
	JSON      json.RawMessage `json:"json,omitempty"`
	Files     []FileEntry     `json:"files,omitempty"`
}

type AdapterCapabilities struct {
	Normalize   bool `json:"normalize"`
	Diff        bool `json:"diff"`
	Materialize bool `json:"materialize"`
	Merge       bool `json:"merge"`
}

type AdapterDescriptor struct {
	Protocol            string              `json:"protocol"`
	ID                  string              `json:"id"`
	Version             string              `json:"version"`
	ContentKind         string              `json:"contentKind"`
	SupportedModes      []string            `json:"supportedModes"`
	DefaultMode         string              `json:"defaultMode"`
	MaxSnapshotBytes    int64               `json:"maxSnapshotBytes"`
	SecretPolicy        string              `json:"secretPolicy"`
	Capabilities        AdapterCapabilities `json:"capabilities"`
	ConfigurationSchema json.RawMessage     `json:"configurationSchema"`
}

type ResourceBinding struct {
	ResourceType     string          `json:"resourceType"`
	Namespace        string          `json:"namespace"`
	Adapter          string          `json:"adapter"`
	AllowedModes     []string        `json:"allowedModes"`
	DefaultMode      string          `json:"defaultMode"`
	ProjectionPolicy string          `json:"projectionPolicy"`
	AdapterConfig    json.RawMessage `json:"adapterConfig,omitempty"`
}

type WorkspaceLimits struct {
	MaxSessionsPerTenant int   `json:"maxSessionsPerTenant"`
	MaxLeaseSeconds      int   `json:"maxLeaseSeconds"`
	MaxSnapshotBytes     int64 `json:"maxSnapshotBytes"`
	MaxOverlayBytes      int64 `json:"maxOverlayBytes"`
}

// EnvironmentProfile is trusted deployment configuration. Consumers select
// an environment and resource identity, never a storage Provider or endpoint.
type EnvironmentProfile struct {
	Protocol string            `json:"protocol"`
	ID       string            `json:"id"`
	Revision uint64            `json:"revision"`
	Bindings []ResourceBinding `json:"bindings"`
	Limits   WorkspaceLimits   `json:"limits"`
}

type AdapterDescribeRequest struct{}

type AdapterDescribeResult struct {
	Adapter AdapterDescriptor `json:"adapter"`
}

type AdapterNormalizeRequest struct {
	Resource      ResourceKey     `json:"resource"`
	Mode          string          `json:"mode"`
	Configuration json.RawMessage `json:"configuration,omitempty"`
	Snapshot      Snapshot        `json:"snapshot"`
}

type AdapterNormalizeResult struct {
	Snapshot Snapshot `json:"snapshot"`
	Digest   string   `json:"digest"`
}

type AdapterDiffRequest struct {
	Resource ResourceKey `json:"resource"`
	Mode     string      `json:"mode"`
	Left     Snapshot    `json:"left"`
	Right    Snapshot    `json:"right"`
}

type ChangeSummary struct {
	Added    int `json:"added"`
	Modified int `json:"modified"`
	Removed  int `json:"removed"`
	Total    int `json:"total"`
}

type AdapterDiffResult struct {
	ChangedPaths []string      `json:"changedPaths,omitempty"`
	Summary      ChangeSummary `json:"summary"`
}

type AdapterMaterializeRequest struct {
	Resource ResourceKey `json:"resource"`
	Mode     string      `json:"mode"`
	Snapshot Snapshot    `json:"snapshot"`
}

// MaterializationRef is opaque outside the Adapter host. It is not a local
// path and cannot be reused after the owning Workspace Lease ends.
type MaterializationRef struct {
	ID     string `json:"id"`
	Digest string `json:"digest"`
	Size   int64  `json:"size"`
}

type AdapterMaterializeResult struct {
	Materialization MaterializationRef `json:"materialization"`
}
