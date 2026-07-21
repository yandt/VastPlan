package pluginv1

import "encoding/json"

// ArtifactRef 唯一定位一个已发布制品。它是制品生产者与消费者之间的稳定 DTO，
// 不包含任何仓库实现细节。
type ArtifactRef struct {
	PluginID string `json:"pluginId"`
	Version  string `json:"version"`
	Channel  string `json:"channel"`
}

// Artifact 是经 schema 验证的可审计制品元数据。
type Artifact struct {
	SchemaVersion string          `json:"schemaVersion"`
	PluginID      string          `json:"pluginId"`
	Version       string          `json:"version"`
	Channel       string          `json:"channel"`
	SHA256        string          `json:"sha256"`
	Size          int64           `json:"size"`
	Object        string          `json:"object"`
	Manifest      json.RawMessage `json:"manifest"`
}

// ArtifactRequirement is one root constraint supplied to the repository resolver.
type ArtifactRequirement struct {
	PluginID   string `json:"pluginId"`
	Constraint string `json:"constraint"`
}

// AvailableCapability describes a capability supplied by the target environment
// rather than by a package selected into this lock.
type AvailableCapability struct {
	Capability string `json:"capability"`
	Version    string `json:"version,omitempty"`
}

// ArtifactResolveRequest contains every policy input that can affect solving.
// SnapshotRevision zero asks the repository to atomically select its current revision.
type ArtifactResolveRequest struct {
	Roots                 []ArtifactRequirement `json:"roots"`
	Target                string                `json:"target"`
	KernelVersion         string                `json:"kernelVersion"`
	Platform              string                `json:"platform,omitempty"`
	AllowedChannels       []string              `json:"allowedChannels"`
	AllowedPublishers     []string              `json:"allowedPublishers"`
	AllowedPluginPrefixes []string              `json:"allowedPluginPrefixes,omitempty"`
	AvailableCapabilities []AvailableCapability `json:"availableCapabilities,omitempty"`
	SnapshotRevision      uint64                `json:"snapshotRevision,omitempty"`
}

// ArtifactLockPackage binds one package identity to its verified repository facts.
type ArtifactLockPackage struct {
	Ref                ArtifactRef          `json:"ref"`
	SHA256             string               `json:"sha256"`
	Size               int64                `json:"size"`
	Publisher          string               `json:"publisher"`
	KeyID              string               `json:"keyId"`
	RepositoryRevision uint64               `json:"repositoryRevision"`
	Dependencies       map[string]string    `json:"dependencies,omitempty"`
	LifecycleStatus    string               `json:"lifecycleStatus,omitempty"`
	LifecycleReason    string               `json:"lifecycleReason,omitempty"`
	Replacement        *ArtifactRequirement `json:"replacement,omitempty"`
}

// ArtifactLock is the immutable, cross-kernel result of one repository solve.
// Digest is SHA-256 over the canonical JSON form with Digest omitted.
type ArtifactLock struct {
	SchemaVersion      string                `json:"schemaVersion"`
	RepositoryRevision uint64                `json:"repositoryRevision"`
	Target             string                `json:"target"`
	KernelVersion      string                `json:"kernelVersion"`
	Platform           string                `json:"platform,omitempty"`
	Roots              []ArtifactRequirement `json:"roots"`
	Packages           []ArtifactLockPackage `json:"packages"`
	Digest             string                `json:"digest"`
}

// ArtifactReference binds one exact immutable artifact to a consumer-owned
// protection snapshot. Purpose is descriptive; owner kind determines authority.
type ArtifactReference struct {
	Ref     ArtifactRef `json:"ref"`
	SHA256  string      `json:"sha256"`
	Purpose string      `json:"purpose"`
}

// ArtifactReferenceSnapshot is a complete replacement snapshot for one owner.
// Tenant and caller identity are supplied by the trusted host, never this DTO.
type ArtifactReferenceSnapshot struct {
	SchemaVersion string              `json:"schemaVersion"`
	OwnerKind     string              `json:"ownerKind"`
	OwnerID       string              `json:"ownerId"`
	Generation    uint64              `json:"generation"`
	TTLSeconds    uint32              `json:"ttlSeconds,omitempty"`
	References    []ArtifactReference `json:"references"`
	Digest        string              `json:"digest"`
}
