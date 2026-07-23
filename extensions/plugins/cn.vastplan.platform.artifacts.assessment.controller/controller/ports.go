package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"cdsoft.com.cn/VastPlan/core/shared/go/artifactassessment"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	"cdsoft.com.cn/VastPlan/core/shared/go/platformadminapi"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

type Ports interface {
	ProviderStatus(context.Context, *contractv1.CallContext) (artifactassessment.ProviderRuntimeStatus, error)
	ListCatalog(context.Context, *contractv1.CallContext, string, int, int) (platformadminapi.ArtifactCatalogPage, error)
	AssessStatus(context.Context, *contractv1.CallContext, artifactassessment.ProviderStatusRequest) ([]byte, error)
	AppendStatus(context.Context, *contractv1.CallContext, artifactassessment.AppendStatusRequest) error
}

type hostPorts struct{ host sdk.Host }

func (p hostPorts) ProviderStatus(ctx context.Context, call *contractv1.CallContext) (artifactassessment.ProviderRuntimeStatus, error) {
	raw, err := callTool(ctx, p.host, call, "platform.artifacts.assessment", "status", []byte(`{}`))
	if err != nil {
		return artifactassessment.ProviderRuntimeStatus{}, err
	}
	var status artifactassessment.ProviderRuntimeStatus
	if err := json.Unmarshal(raw, &status); err != nil || artifactassessment.ValidateProviderRuntimeStatus(status) != nil {
		return artifactassessment.ProviderRuntimeStatus{}, errors.New("Assessment Provider runtime status 无效")
	}
	return status, nil
}

func (p hostPorts) ListCatalog(ctx context.Context, call *contractv1.CallContext, channel string, page, pageSize int) (platformadminapi.ArtifactCatalogPage, error) {
	payload, _ := json.Marshal(map[string]any{"channel": channel, "lifecycle": "active", "page": page, "pageSize": pageSize})
	raw, err := callTool(ctx, p.host, call, "platform.artifacts.repository", "listCatalog", payload)
	if err != nil {
		return platformadminapi.ArtifactCatalogPage{}, err
	}
	var result platformadminapi.ArtifactCatalogPage
	if err := json.Unmarshal(raw, &result); err != nil || result.Page != page || result.PageSize < 1 || result.PageSize > pageSize || result.Total < len(result.Items) {
		return platformadminapi.ArtifactCatalogPage{}, errors.New("Repository Catalog page 无效")
	}
	return result, nil
}

func (p hostPorts) AssessStatus(ctx context.Context, call *contractv1.CallContext, request artifactassessment.ProviderStatusRequest) ([]byte, error) {
	payload, _ := json.Marshal(request)
	raw, err := callTool(ctx, p.host, call, "platform.artifacts.assessment", "assessStatus", payload)
	if err != nil {
		return nil, err
	}
	record, _, err := artifactassessment.InspectStatus(raw)
	if err != nil {
		return nil, errors.New("Assessment Provider 返回无效 StatusRecord")
	}
	if record.Sequence != request.Sequence || record.AdmissionSHA256 != request.AdmissionSHA256 || record.PreviousSHA256 != request.PreviousSHA256 || record.Evaluation.SubjectSHA256 != request.SubjectSHA256 || record.Evaluation.SBOMSHA256 != request.SBOMSHA256 {
		return nil, errors.New("Assessment Provider StatusRecord 未绑定请求")
	}
	return raw, nil
}

func (p hostPorts) AppendStatus(ctx context.Context, call *contractv1.CallContext, request artifactassessment.AppendStatusRequest) error {
	payload, _ := json.Marshal(request)
	_, err := callTool(ctx, p.host, call, "platform.artifacts.repository", "appendAssessmentStatus", payload)
	return err
}

func callTool(ctx context.Context, host sdk.Host, call *contractv1.CallContext, capability, operation string, payload []byte) ([]byte, error) {
	result, raw, err := host.Call(ctx, &contractv1.CallTarget{ExtensionPoint: extpoint.ToolPackage, Capability: capability, Operation: &operation}, call, payload)
	if err != nil {
		return nil, fmt.Errorf("调用 %s/%s 失败", capability, operation)
	}
	if result == nil || result.GetStatus() != contractv1.CallResult_STATUS_OK {
		return nil, fmt.Errorf("调用 %s/%s 被拒绝", capability, operation)
	}
	return raw, nil
}
