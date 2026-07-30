package portalcomposer

import (
	"context"
	"fmt"
	"strings"

	"cdsoft.com.cn/VastPlan/extensions/libraries/go/portalapi"
)

func (s *Service) handlePortalOperation(ctx context.Context, principal portalapi.Principal, operation string, payload []byte) (any, error) {
	switch operation {
	case "createPortal", "createPortalWorkingCopy", "savePortalWorkingCopy", "submitPortalPublication", "approvePortalPublication", "publishPortalPublication", "releasePortalPublication", "portalVersionHistory", "readPortalVersion", "comparePortalVersions", "restorePortalVersion", "createPortalVersion", "updatePortalVersion", "deletePortalVersion", "submitPortalVersion", "approvePortalVersion", "publishPortalVersion", "releasePortalVersion", "rollbackPortalRelease", "portalGovernance", "listPortalReleases", "audit":
		return s.handlePortalAggregateOperation(ctx, principal, operation, payload)
	case "listTestTargetBindings", "putTestTargetBinding", "listTestReleases", "createTestRelease", "rollbackTestRelease":
		return s.handleTestReleaseOperation(ctx, principal, operation, payload)
	default:
		return nil, fmt.Errorf("不支持 Portal Composer 操作 %q", operation)
	}
}

func (s *Service) handlePortalAggregateOperation(ctx context.Context, principal portalapi.Principal, operation string, payload []byte) (any, error) {
	switch operation {
	case "portalVersionHistory", "readPortalVersion", "comparePortalVersions", "restorePortalVersion":
		return s.handlePortalVersionControlOperation(ctx, principal, operation, payload)
	}
	switch operation {
	case "portalGovernance":
		return s.PortalGovernance(ctx, principal)
	case "listPortalReleases":
		return s.ListPortalReleases(ctx, principal)
	case "createPortal":
		var request portalapi.PortalVersionRequest
		if err := decode(payload, &request); err != nil {
			return nil, err
		}
		return s.CreatePortal(ctx, principal, request)
	case "createPortalWorkingCopy":
		var request portalapi.PortalVersionRequest
		if err := decode(payload, &request); err != nil {
			return nil, err
		}
		return s.CreatePortalWorkingCopy(ctx, principal, request.PortalID, request.Configuration)
	case "savePortalWorkingCopy":
		var request struct {
			PortalID    string                                 `json:"portalId"`
			WorkingCopy portalapi.SavePortalWorkingCopyRequest `json:"workingCopy"`
		}
		if err := decode(payload, &request); err != nil {
			return nil, err
		}
		return s.SavePortalWorkingCopy(ctx, principal, request.PortalID, request.WorkingCopy)
	case "submitPortalPublication":
		var request struct {
			PortalID    string                                   `json:"portalId"`
			Publication portalapi.SubmitPortalPublicationRequest `json:"publication"`
		}
		if err := decode(payload, &request); err != nil {
			return nil, err
		}
		return s.SubmitPortalPublication(ctx, principal, request.PortalID, request.Publication)
	case "approvePortalPublication", "publishPortalPublication":
		var request struct {
			PortalID      string `json:"portalId"`
			PublicationID uint64 `json:"publicationId"`
		}
		if err := decode(payload, &request); err != nil {
			return nil, err
		}
		if operation == "approvePortalPublication" {
			return s.ApprovePortalPublication(ctx, principal, request.PortalID, request.PublicationID)
		}
		return s.PublishPortalPublication(ctx, principal, request.PortalID, request.PublicationID)
	case "releasePortalPublication":
		var request struct {
			PortalID string                                    `json:"portalId"`
			Release  portalapi.PortalPublicationReleaseRequest `json:"release"`
		}
		if err := decode(payload, &request); err != nil {
			return nil, err
		}
		return s.ReleasePortalPublication(ctx, principal, request.PortalID, request.Release)
	case "createPortalVersion":
		var request portalapi.PortalVersionRequest
		if err := decode(payload, &request); err != nil {
			return nil, err
		}
		return s.CreatePortalVersion(ctx, principal, request.PortalID, request.Configuration)
	case "updatePortalVersion":
		var request struct {
			PortalID      string                        `json:"portalId"`
			VersionID     uint64                        `json:"versionId"`
			Configuration portalapi.PortalConfiguration `json:"configuration"`
		}
		if err := decode(payload, &request); err != nil {
			return nil, err
		}
		return s.UpdatePortalVersion(ctx, principal, request.PortalID, request.VersionID, request.Configuration)
	case "deletePortalVersion":
		var request struct {
			PortalID  string `json:"portalId"`
			VersionID uint64 `json:"versionId"`
		}
		if err := decode(payload, &request); err != nil {
			return nil, err
		}
		return s.DeletePortalVersion(ctx, principal, request.PortalID, request.VersionID)
	case "submitPortalVersion", "approvePortalVersion", "publishPortalVersion":
		var request struct {
			PortalID         string `json:"portalId"`
			VersionID        uint64 `json:"versionId"`
			BreakGlassReason string `json:"breakGlassReason,omitempty"`
		}
		if err := decode(payload, &request); err != nil {
			return nil, err
		}
		if operation == "publishPortalVersion" && principal.System {
			return s.breakGlassPublishPortalVersion(ctx, principal, request.PortalID, request.VersionID, request.BreakGlassReason)
		}
		return s.TransitionPortalVersion(ctx, principal, request.PortalID, request.VersionID, strings.TrimSuffix(operation, "PortalVersion"))
	case "releasePortalVersion":
		var request struct {
			PortalID string                         `json:"portalId"`
			Release  portalapi.PortalReleaseRequest `json:"release"`
		}
		if err := decode(payload, &request); err != nil {
			return nil, err
		}
		return s.ReleasePortalVersion(ctx, principal, request.PortalID, request.Release)
	case "rollbackPortalRelease":
		var request struct {
			PortalID                 string `json:"portalId"`
			ReleaseID                uint64 `json:"releaseId"`
			ExpectedCurrentReleaseID uint64 `json:"expectedCurrentReleaseId"`
			Reason                   string `json:"reason"`
		}
		if err := decode(payload, &request); err != nil {
			return nil, err
		}
		return s.RollbackPortalRelease(ctx, principal, request.PortalID, request.ReleaseID, request.ExpectedCurrentReleaseID, request.Reason)
	case "audit":
		var request struct {
			PortalID   string `json:"portalId"`
			RevisionID uint64 `json:"revisionId"`
		}
		if err := decode(payload, &request); err != nil {
			return nil, err
		}
		return s.Audit(ctx, principal, request.PortalID, request.RevisionID)
	default:
		return nil, fmt.Errorf("不支持 Portal 聚合操作 %q", operation)
	}
}

func (s *Service) handlePortalVersionControlOperation(ctx context.Context, principal portalapi.Principal, operation string, payload []byte) (any, error) {
	switch operation {
	case "portalVersionHistory":
		var request struct {
			PortalID string `json:"portalId"`
		}
		if err := decode(payload, &request); err != nil {
			return nil, err
		}
		return s.PortalVersionHistory(ctx, principal, request.PortalID)
	case "readPortalVersion":
		var request struct {
			PortalID  string `json:"portalId"`
			VersionID string `json:"versionId"`
		}
		if err := decode(payload, &request); err != nil {
			return nil, err
		}
		return s.ReadPortalVersion(ctx, principal, request.PortalID, request.VersionID)
	case "comparePortalVersions":
		var request struct {
			PortalID       string `json:"portalId"`
			LeftVersionID  string `json:"leftVersionId"`
			RightVersionID string `json:"rightVersionId"`
		}
		if err := decode(payload, &request); err != nil {
			return nil, err
		}
		return s.ComparePortalVersions(ctx, principal, request.PortalID, request.LeftVersionID, request.RightVersionID)
	case "restorePortalVersion":
		var request struct {
			PortalID string                                `json:"portalId"`
			Restore  portalapi.RestorePortalVersionRequest `json:"restore"`
		}
		if err := decode(payload, &request); err != nil {
			return nil, err
		}
		return s.RestorePortalVersion(ctx, principal, request.PortalID, request.Restore)
	default:
		return nil, fmt.Errorf("不支持 Portal 版本控制操作 %q", operation)
	}
}

func (s *Service) handleTestReleaseOperation(ctx context.Context, principal portalapi.Principal, operation string, payload []byte) (any, error) {
	switch operation {
	case "listTestTargetBindings":
		return s.ListTestTargetBindings(ctx, principal)
	case "putTestTargetBinding":
		var request struct {
			ID      string                                `json:"id"`
			Binding portalapi.PutTestTargetBindingRequest `json:"binding"`
		}
		if err := decode(payload, &request); err != nil {
			return nil, err
		}
		return s.PutTestTargetBinding(ctx, principal, request.ID, request.Binding)
	case "listTestReleases":
		return s.ListTestReleases(ctx, principal)
	case "createTestRelease":
		var request portalapi.CreateTestReleaseRequest
		if err := decode(payload, &request); err != nil {
			return nil, err
		}
		return s.CreateTestRelease(ctx, principal, request)
	case "rollbackTestRelease":
		var request struct {
			ID uint64 `json:"id"`
		}
		if err := decode(payload, &request); err != nil {
			return nil, err
		}
		return s.RollbackTestRelease(ctx, principal, request.ID)
	default:
		return nil, fmt.Errorf("不支持 Portal 测试发布操作 %q", operation)
	}
}
