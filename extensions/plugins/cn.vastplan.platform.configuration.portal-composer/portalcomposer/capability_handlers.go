package portalcomposer

import (
	"context"
	"fmt"
	"strings"

	"cdsoft.com.cn/VastPlan/extensions/libraries/go/portalapi"
)

func (s *Service) handlePortalOperation(ctx context.Context, principal portalapi.Principal, operation string, payload []byte) (any, error) {
	switch operation {
	case "createPortal", "createPortalVersion", "updatePortalVersion", "deletePortalVersion", "submitPortalVersion", "approvePortalVersion", "publishPortalVersion", "releasePortalVersion", "rollbackPortalRelease", "portalGovernance", "listPortalReleases", "audit":
		return s.handlePortalAggregateOperation(ctx, principal, operation, payload)
	case "listTestTargetBindings", "putTestTargetBinding", "listTestReleases", "createTestRelease", "rollbackTestRelease":
		return s.handleTestReleaseOperation(ctx, principal, operation, payload)
	default:
		return nil, fmt.Errorf("不支持 Portal Composer 操作 %q", operation)
	}
}

func (s *Service) handlePortalAggregateOperation(ctx context.Context, principal portalapi.Principal, operation string, payload []byte) (any, error) {
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
