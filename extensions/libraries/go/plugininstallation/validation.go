package plugininstallation

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

var (
	ErrInvalid              = errors.New("插件安装意图无效")
	ErrUntrustedSource      = errors.New("插件安装来源没有经过可信宿主选择")
	ErrTargetScopeMismatch  = errors.New("插件安装目标超出当前来源绑定范围")
	ErrDevelopmentForbidden = errors.New("当前调用不允许使用开发自动安装来源")
)

func ValidatePreviewRequest(request PreviewRequest) (PreviewRequest, error) {
	if request.Version != ProtocolVersion || request.Target.Kernel != "backend" ||
		strings.TrimSpace(request.Target.Deployment) == "" || strings.TrimSpace(request.Target.UnitID) == "" ||
		strings.TrimSpace(request.Change.PluginID) == "" {
		return PreviewRequest{}, ErrInvalid
	}
	request.Target.Deployment = strings.TrimSpace(request.Target.Deployment)
	request.Target.UnitID = strings.TrimSpace(request.Target.UnitID)
	request.Change.PluginID = strings.TrimSpace(request.Change.PluginID)
	if request.PortalTargets == nil || len(request.PortalTargets) > 32 {
		return PreviewRequest{}, ErrInvalid
	}
	seenPortals := map[string]struct{}{}
	for index, portalID := range request.PortalTargets {
		portalID = strings.TrimSpace(portalID)
		if portalID == "" || len(portalID) > 160 || strings.ContainsAny(portalID, "/\\\x00") {
			return PreviewRequest{}, ErrInvalid
		}
		if _, duplicate := seenPortals[portalID]; duplicate {
			return PreviewRequest{}, ErrInvalid
		}
		seenPortals[portalID] = struct{}{}
		request.PortalTargets[index] = portalID
	}
	sort.Strings(request.PortalTargets)
	switch request.Change.Action {
	case ActionInstall, ActionUpgrade:
		if request.Change.Requirement == nil || request.Change.Requirement.PluginID != request.Change.PluginID {
			return PreviewRequest{}, ErrInvalid
		}
		requirement, err := pluginv1.NormalizeArtifactRequirement(*request.Change.Requirement)
		if err != nil {
			return PreviewRequest{}, fmt.Errorf("%w: %v", ErrInvalid, err)
		}
		request.Change.Requirement = &requirement
	case ActionRemove:
		if request.Change.Requirement != nil {
			return PreviewRequest{}, ErrInvalid
		}
	default:
		return PreviewRequest{}, ErrInvalid
	}
	return request, nil
}

func ValidSource(source Source) bool {
	switch source {
	case SourceController, SourceSelfService, SourceDevelopment:
		return true
	default:
		return false
	}
}
