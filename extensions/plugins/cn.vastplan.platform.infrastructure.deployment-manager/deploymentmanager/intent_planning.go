package deploymentmanager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/deploymentpublication"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/extpoint"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

var (
	errPlanStale    = errors.New("Application Intent 规划快照已过期")
	errPlanNotReady = errors.New("Application Intent 规划尚未收敛为 Resolved")
)

type intentPlan struct {
	report  backendcompositionv1.ResolutionReport
	preview *deploymentpublication.Result
}

func normalizeApplicationIntent(input backendcompositionv1.ApplicationIntent, tenantID string, revision uint64) (backendcompositionv1.ApplicationIntent, error) {
	name := strings.TrimSpace(input.Metadata.Name)
	if name == "" {
		return backendcompositionv1.ApplicationIntent{}, errInvalid
	}
	input.Document = compositioncommonv1.Document{Version: 1, Revision: revision, ID: name}
	input.Target = compositioncommonv1.Target{Kernel: compositioncommonv1.KernelBackend}
	input.Metadata.Name, input.Metadata.Tenant = name, tenantID
	return backendcompositionv1.ValidateApplicationIntent(input)
}

func buildIntentPlan(ctx context.Context, host sdk.Host, call *contractv1.CallContext, intent backendcompositionv1.ApplicationIntent, snapshot *backendcompositionv1.PlanningConfigurationSnapshot, revision uint64) (intentPlan, error) {
	profile, err := trustedPlanningProfile(ctx, host, call, intent.Metadata.Name)
	if err != nil {
		return intentPlan{}, err
	}
	request := backendcompositionv1.PlanningRequest{Intent: intent, PlatformProfile: profile, ConfigurationSnapshot: snapshot}
	report, err := callCompositionPlanner(ctx, host, call, request)
	if err != nil {
		return intentPlan{}, err
	}
	if report.Intent.ID != intent.ID || report.Intent.Revision != intent.Revision || report.Intent.Digest != intent.Digest() || report.PlatformProfile.ID != profile.ID || report.PlatformProfile.Revision != profile.Revision || report.PlatformProfile.Digest != profile.Digest() {
		return intentPlan{}, errors.New("Composition Planner 返回了不匹配的输入身份")
	}
	if report.Status != backendcompositionv1.ResolutionResolved {
		return intentPlan{report: report}, nil
	}
	if report.ApplicationComposition == nil {
		return intentPlan{}, errors.New("Resolved 规划缺少 Application Composition")
	}
	preview, err := previewService(ctx, host, call, *report.ApplicationComposition, revision)
	if err != nil {
		return intentPlan{}, err
	}
	if preview.PlatformProfile != report.PlatformProfile || preview.Deployment.Resolution.ApplicationComposition.Digest != report.ApplicationCompositionDigest {
		return intentPlan{}, errPlanStale
	}
	return intentPlan{report: report, preview: &preview}, nil
}

func trustedPlanningProfile(ctx context.Context, host sdk.Host, call *contractv1.CallContext, deployment string) (backendcompositionv1.PlatformProfile, error) {
	var response struct {
		Items []deploymentpublication.Target `json:"items"`
	}
	if err := callKernelDeployment(ctx, host, call, deploymentpublication.KernelTargetsService, struct{}{}, &response); err != nil {
		return backendcompositionv1.PlatformProfile{}, err
	}
	var matched *deploymentpublication.Target
	for index := range response.Items {
		if response.Items[index].DeploymentName != deployment {
			continue
		}
		if matched != nil {
			return backendcompositionv1.PlatformProfile{}, errors.New("平台目录返回重复部署规划目标")
		}
		matched = &response.Items[index]
	}
	if matched == nil {
		return backendcompositionv1.PlatformProfile{}, errNotFound
	}
	profile, err := backendcompositionv1.ValidatePlatformProfile(matched.PlanningProfile)
	if err != nil || matched.PlatformProfile.ID != profile.ID || matched.PlatformProfile.Revision != profile.Revision || matched.PlatformProfile.Digest != profile.Digest() {
		return backendcompositionv1.PlatformProfile{}, errors.New("平台目录返回的规划 Profile 与绑定摘要不一致")
	}
	return profile, nil
}

func callCompositionPlanner(ctx context.Context, host sdk.Host, call *contractv1.CallContext, request backendcompositionv1.PlanningRequest) (backendcompositionv1.ResolutionReport, error) {
	if host == nil || call == nil {
		return backendcompositionv1.ResolutionReport{}, errPlanNotReady
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return backendcompositionv1.ResolutionReport{}, err
	}
	operation := backendcompositionv1.PlanningOperation
	result, raw, err := host.Call(ctx, &contractv1.CallTarget{ExtensionPoint: extpoint.ToolPackage, Capability: backendcompositionv1.PlanningCapability, Operation: &operation}, call, payload)
	if err != nil || result == nil || result.Status != contractv1.CallResult_STATUS_OK {
		return backendcompositionv1.ResolutionReport{}, fmt.Errorf("调用 Composition Planner: %w", errPlanNotReady)
	}
	var report backendcompositionv1.ResolutionReport
	if err := json.Unmarshal(raw, &report); err != nil {
		return backendcompositionv1.ResolutionReport{}, errors.New("Composition Planner 响应不是有效 JSON")
	}
	return backendcompositionv1.ValidateResolutionReport(report)
}

func sameIntentPlan(revisionPlan backendcompositionv1.ResolutionReport, previewDigest string, current intentPlan) bool {
	if revisionPlan.PlanDigest == "" || revisionPlan.PlanDigest != current.report.PlanDigest {
		return false
	}
	if current.report.Status != backendcompositionv1.ResolutionResolved {
		return previewDigest == ""
	}
	return current.preview != nil && previewDigest == current.preview.Digest
}
