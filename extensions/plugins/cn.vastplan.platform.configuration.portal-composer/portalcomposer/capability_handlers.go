package portalcomposer

import (
	"context"
	"fmt"

	workflowv1 "cdsoft.com.cn/VastPlan/contracts/schemas/workflow/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/portalapi"
)

func (s *Service) handlePortalOperation(ctx context.Context, principal portalapi.Principal, operation string, payload []byte) (any, error) {
	switch operation {
	case portalapi.PreparePortalPublicationOperation:
		return s.PreparePortalPublication(ctx, principal, payload)
	case portalapi.ExecutePublicationReleaseOperation:
		var request workflowv1.ActionRequest
		if err := decode(payload, &request); err != nil {
			return nil, err
		}
		return s.ExecutePublicationRelease(ctx, principal, request)
	case portalapi.PreparePluginInstallationOperation, portalapi.CommitPluginInstallationOperation, portalapi.AbortPluginInstallationOperation, portalapi.RollbackPluginInstallationOperation:
		return s.handlePluginInstallationOperation(ctx, principal, operation, payload)
	case portalapi.ReadNavigationConfigurationOperation, portalapi.PrepareNavigationConfigurationOperation, portalapi.CommitNavigationConfigurationOperation, portalapi.AbortNavigationConfigurationOperation, portalapi.RollbackNavigationConfigurationOperation:
		return s.handleNavigationConfigurationOperation(ctx, principal, operation, payload)
	case "createPortal", "createPortalWorkingCopy", "savePortalWorkingCopy", "submitPortalPublication", "approvePortalPublication", "publishPortalPublication", "releasePortalPublication", "portalVersionHistory", "readPortalVersion", "comparePortalVersions", "restorePortalVersion", "rollbackPortalRelease", "portalGovernance", "listPortalReleases", "audit":
		return s.handlePortalAggregateOperation(ctx, principal, operation, payload)
	case "listTestTargetBindings", "putTestTargetBinding", "listTestReleases", "createTestRelease", "rollbackTestRelease":
		return s.handleTestReleaseOperation(ctx, principal, operation, payload)
	default:
		return nil, fmt.Errorf("不支持 Portal Composer 操作 %q", operation)
	}
}

func (s *Service) handleNavigationConfigurationOperation(ctx context.Context, principal portalapi.Principal, operation string, payload []byte) (any, error) {
	switch operation {
	case portalapi.ReadNavigationConfigurationOperation:
		var request portalapi.NavigationConfigurationLookup
		if err := decode(payload, &request); err != nil {
			return nil, err
		}
		return s.ReadNavigationConfiguration(ctx, principal, request.PortalID, request.ServiceID)
	case portalapi.PrepareNavigationConfigurationOperation:
		var request portalapi.NavigationConfigurationRequest
		if err := decode(payload, &request); err != nil {
			return nil, err
		}
		return s.PrepareNavigationConfiguration(ctx, principal, request)
	case portalapi.CommitNavigationConfigurationOperation, portalapi.AbortNavigationConfigurationOperation, portalapi.RollbackNavigationConfigurationOperation:
		var request portalapi.NavigationConfigurationLookup
		if err := decode(payload, &request); err != nil {
			return nil, err
		}
		switch operation {
		case portalapi.CommitNavigationConfigurationOperation:
			return s.CommitNavigationConfiguration(ctx, principal, request)
		case portalapi.AbortNavigationConfigurationOperation:
			return s.AbortNavigationConfiguration(ctx, principal, request)
		default:
			return s.RollbackNavigationConfiguration(ctx, principal, request)
		}
	default:
		return nil, fmt.Errorf("不支持 Portal 导航编排操作 %q", operation)
	}
}

func (s *Service) handlePluginInstallationOperation(ctx context.Context, principal portalapi.Principal, operation string, payload []byte) (any, error) {
	switch operation {
	case portalapi.PreparePluginInstallationOperation:
		var request portalapi.PluginInstallationRequest
		if err := decode(payload, &request); err != nil {
			return nil, err
		}
		return s.PreparePluginInstallation(ctx, principal, request)
	case portalapi.CommitPluginInstallationOperation, portalapi.AbortPluginInstallationOperation, portalapi.RollbackPluginInstallationOperation:
		var request portalapi.PluginInstallationLookup
		if err := decode(payload, &request); err != nil {
			return nil, err
		}
		switch operation {
		case portalapi.CommitPluginInstallationOperation:
			return s.CommitPluginInstallation(ctx, principal, request)
		case portalapi.AbortPluginInstallationOperation:
			return s.AbortPluginInstallation(ctx, principal, request)
		default:
			return s.RollbackPluginInstallation(ctx, principal, request)
		}
	default:
		return nil, fmt.Errorf("不支持 Portal 插件安装操作 %q", operation)
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
		var request portalapi.CreatePortalRequest
		if err := decode(payload, &request); err != nil {
			return nil, err
		}
		return s.CreatePortal(ctx, principal, request)
	case "createPortalWorkingCopy":
		var request portalapi.CreatePortalRequest
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
			PortalID      string                          `json:"portalId"`
			PublicationID uint64                          `json:"publicationId"`
			Approval      portalapi.PortalApprovalRequest `json:"approval"`
		}
		if err := decode(payload, &request); err != nil {
			return nil, err
		}
		if operation == "approvePortalPublication" {
			return s.ApprovePortalPublication(ctx, principal, request.PortalID, request.PublicationID, request.Approval)
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
