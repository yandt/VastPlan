package portalcomposer

import (
	"context"
	"errors"
	"strings"

	versioningv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versioning/v1"
	versionresourcev1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionresource/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/portalapi"
)

var ErrVersionControlUnavailable = errors.New("Portal 版本控制不可用")

type PortalVersionControlBinding struct {
	EnvironmentID string `json:"environmentId"`
	ResourceType  string `json:"resourceType"`
}

func (b PortalVersionControlBinding) validate() error {
	if strings.TrimSpace(b.EnvironmentID) == "" || strings.TrimSpace(b.ResourceType) == "" {
		return errors.New("Portal 版本控制绑定必须包含 environmentId 和 resourceType")
	}
	return nil
}

type PortalVersionControlCapabilities struct {
	Read    bool `json:"read"`
	Diff    bool `json:"diff"`
	Restore bool `json:"restore"`
}

type PortalVersionCommitRequest struct {
	PortalID      string
	Binding       PortalVersionControlBinding
	BaseRef       *versioningv1.VersionRef
	OperationID   string
	ActorID       string
	Configuration portalapi.PortalConfiguration
}

type PortalVersionCommitResult struct {
	EnvironmentDigest string
	VersionRef        versioningv1.VersionRef
	Capabilities      PortalVersionControlCapabilities
}

type PortalVersionReadRequest struct {
	PortalID          string
	Binding           PortalVersionControlBinding
	EnvironmentDigest string
	VersionRef        versioningv1.VersionRef
}

type PortalVersionCompareRequest struct {
	PortalID          string
	Binding           PortalVersionControlBinding
	EnvironmentDigest string
	Left              versioningv1.VersionRef
	Right             versioningv1.VersionRef
}

type PortalVersionCompareResult struct {
	Dirty         bool
	DiffAvailable bool
	ChangedPaths  []string
	Summary       versionresourcev1.ChangeSummary
}

// PortalVersionControl is the domain-neutral port used by Portal Composer.
// Implementations may use Workspace, a future Git service, or another trusted
// backend without leaking that backend into the Portal aggregate.
type PortalVersionControl interface {
	Describe(context.Context, PortalVersionControlBinding, string) (PortalVersionControlCapabilities, error)
	Commit(context.Context, PortalVersionCommitRequest) (PortalVersionCommitResult, error)
	Read(context.Context, PortalVersionReadRequest) (portalapi.PortalConfiguration, error)
	Compare(context.Context, PortalVersionCompareRequest) (PortalVersionCompareResult, error)
}

type versionControlContextKey struct{}

func withVersionControl(ctx context.Context, control PortalVersionControl) context.Context {
	return context.WithValue(ctx, versionControlContextKey{}, control)
}

func versionControlFromContext(ctx context.Context) (PortalVersionControl, error) {
	control, _ := ctx.Value(versionControlContextKey{}).(PortalVersionControl)
	if control == nil {
		return nil, ErrVersionControlUnavailable
	}
	return control, nil
}

type portalVersionControlState struct {
	Binding      PortalVersionControlBinding      `json:"binding"`
	LatestRef    *versioningv1.VersionRef         `json:"latestVersionRef,omitempty"`
	Capabilities PortalVersionControlCapabilities `json:"capabilities"`
	History      []portalVersionHistoryRecord     `json:"history"`
	Pending      *portalVersionPendingOperation   `json:"pending,omitempty"`
}

type portalVersionHistoryRecord struct {
	Entry       portalapi.PortalVersionHistoryEntry `json:"entry"`
	OperationID string                              `json:"operationId"`
}

type portalVersionPendingOperation struct {
	OperationID     string `json:"operationId"`
	PublicationID   uint64 `json:"publicationId"`
	WorkingRevision uint64 `json:"workingRevision"`
	Digest          string `json:"digest"`
}
