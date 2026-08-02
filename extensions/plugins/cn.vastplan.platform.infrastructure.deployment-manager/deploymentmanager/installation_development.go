package deploymentmanager

import (
	"context"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/plugininstallation"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

// ApplyDevelopmentPluginInstallation keeps development automation on the same
// durable candidate, approval projection and Generation publication chain used
// by production. Source authorization and explicit target binding are still
// enforced by CreatePluginInstallationCandidate.
func (s *Service) ApplyDevelopmentPluginInstallation(ctx context.Context, host sdk.Host, call *contractv1.CallContext, request plugininstallation.PreviewRequest) (plugininstallation.Candidate, error) {
	candidate, err := s.CreatePluginInstallationCandidate(ctx, host, call, plugininstallation.SourceDevelopment, request)
	if err != nil {
		return plugininstallation.Candidate{}, err
	}
	candidate, err = s.SubmitPluginInstallationCandidate(ctx, host, call, candidate.ID)
	if err != nil {
		return candidate, err
	}
	return s.ActivatePluginInstallationCandidate(ctx, host, call, candidate.ID)
}
