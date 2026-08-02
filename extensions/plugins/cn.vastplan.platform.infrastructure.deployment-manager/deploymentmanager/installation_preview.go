package deploymentmanager

import (
	"context"
	"errors"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/platformadminapi"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/plugininstallation"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

var (
	errInstallationUnsupported = errors.New("活动服务尚未使用 Application Intent，不能生成插件安装预览")
)

// PreviewPluginInstallation projects one root-plugin mutation through the
// existing Planner and kernel preview path. It is deliberately read-only: no
// revision, artifact reference or activation is persisted here.
func (s *Service) PreviewPluginInstallation(ctx context.Context, host sdk.Host, call *contractv1.CallContext, source plugininstallation.Source, input plugininstallation.PreviewRequest) (plugininstallation.Preview, error) {
	request, err := plugininstallation.ValidatePreviewRequest(input)
	if err != nil {
		return plugininstallation.Preview{}, err
	}
	if err := authorizeInstallationSource(call, source); err != nil {
		return plugininstallation.Preview{}, err
	}
	tenant, err := callTenant(call)
	if err != nil {
		return plugininstallation.Preview{}, err
	}

	s.mu.Lock()
	state := s.tenantLocked(tenant)
	active, err := activeServiceRevision(state, request.Target.Deployment)
	candidateRevision := state.NextRevision + 1
	developmentBound := source != plugininstallation.SourceDevelopment || developmentInstallationBound(state, request)
	s.mu.Unlock()
	if err != nil {
		return plugininstallation.Preview{}, err
	}
	if !developmentBound {
		return plugininstallation.Preview{}, plugininstallation.ErrTargetScopeMismatch
	}
	if request.ExpectedActiveRevision != 0 && request.ExpectedActiveRevision != active.ID {
		return plugininstallation.Preview{}, errVersionConflict
	}
	if active.Intent == nil || active.ResolutionReport == nil || active.ResolutionReport.ArtifactLock == nil {
		return plugininstallation.Preview{}, errInstallationUnsupported
	}

	candidate := cloneJSON(*active.Intent)
	if err := applyInstallationChange(&candidate, request); err != nil {
		return plugininstallation.Preview{}, err
	}
	candidate, err = normalizeApplicationIntent(candidate, tenant, candidateRevision)
	if err != nil {
		return plugininstallation.Preview{}, err
	}
	plan, err := buildIntentPlan(ctx, host, call, candidate, active.ConfigurationSnapshot, candidateRevision)
	if err != nil {
		return plugininstallation.Preview{}, err
	}
	preview, err := buildInstallationPreview(source, request, active, candidate, plan)
	if err != nil {
		return plugininstallation.Preview{}, err
	}
	return preview, nil
}

func buildInstallationPreview(source plugininstallation.Source, request plugininstallation.PreviewRequest, active platformadminapi.ServiceRevision, candidate backendcompositionv1.ApplicationIntent, plan intentPlan) (plugininstallation.Preview, error) {
	currentLock := active.ResolutionReport.ArtifactLock
	changes := diffArtifactLocks(currentLock, plan.report.ArtifactLock)
	rootChanged := installationRootChanged(*active.Intent, candidate, request.Target.UnitID, request.Change.PluginID)
	if err := requireApplicationManagedPlugin(request.Change.PluginID, source, currentLock, plan.report.ArtifactLock); err != nil {
		return plugininstallation.Preview{}, err
	}
	gaps := make([]plugininstallation.ConfigurationGap, 0)
	for _, item := range plan.report.ConfigurationPlan.Items {
		if len(item.Missing) == 0 {
			continue
		}
		gaps = append(gaps, plugininstallation.ConfigurationGap{UnitID: item.UnitID, PluginID: item.PluginID, Missing: append([]backendcompositionv1.ConfigurationRequirement(nil), item.Missing...)})
	}
	status := plugininstallation.Status(plan.report.Status)
	result := plugininstallation.Preview{
		Version: plugininstallation.ProtocolVersion, Source: source, Status: status,
		Target: request.Target, Action: request.Change.Action, PluginID: request.Change.PluginID,
		ActiveRevision: active.ID, CandidateRevision: candidate.Revision, CandidateIntentDigest: candidate.Digest(),
		PlanDigest: plan.report.PlanDigest, PlatformProfile: plan.report.PlatformProfile,
		ArtifactLock: cloneArtifactLock(plan.report.ArtifactLock), Changes: changes, ConfigurationGaps: gaps,
		Diagnostics: append([]backendcompositionv1.ResolutionDiagnostic(nil), plan.report.Diagnostics...),
		Impact: plugininstallation.Impact{
			ApplyStrategy: plugininstallation.ApplyServiceGeneration, RequiresApproval: source != plugininstallation.SourceDevelopment,
			KernelRestartRequired: false, RootChanged: rootChanged, Noop: !rootChanged && len(changes) == 0,
		},
	}
	if plan.report.ArtifactLock != nil {
		result.RepositoryRevision = plan.report.ArtifactLock.RepositoryRevision
	}
	if plan.preview != nil {
		result.PreviewDigest = plan.preview.Digest
	}
	return result, nil
}
