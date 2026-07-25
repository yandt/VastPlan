package planner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/extpoint"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

const repositoryCapability = "platform.artifacts.repository"

type Repository interface {
	Describe(context.Context, pluginv1.ArtifactPlanningRequest) (pluginv1.ArtifactPlanningResponse, error)
	Resolve(context.Context, pluginv1.ArtifactResolveRequest) (pluginv1.ArtifactLock, error)
}

type HostRepository struct {
	Host    sdk.Host
	Context *contractv1.CallContext
}

func (r HostRepository) Describe(ctx context.Context, request pluginv1.ArtifactPlanningRequest) (pluginv1.ArtifactPlanningResponse, error) {
	var response pluginv1.ArtifactPlanningResponse
	if err := r.call(ctx, "describePlanning", request, &response); err != nil {
		return pluginv1.ArtifactPlanningResponse{}, err
	}
	return pluginv1.ValidateArtifactPlanningResponse(response)
}

func (r HostRepository) Resolve(ctx context.Context, request pluginv1.ArtifactResolveRequest) (pluginv1.ArtifactLock, error) {
	var response pluginv1.ArtifactLock
	if err := r.call(ctx, "resolve", request, &response); err != nil {
		return pluginv1.ArtifactLock{}, err
	}
	raw, err := json.Marshal(response)
	if err != nil {
		return pluginv1.ArtifactLock{}, err
	}
	if err := pluginv1.ValidateArtifactLock(raw); err != nil {
		return pluginv1.ArtifactLock{}, err
	}
	digest, err := pluginv1.ArtifactLockDigest(response)
	if err != nil || digest != response.Digest {
		return pluginv1.ArtifactLock{}, errors.New("仓库返回的 Artifact Lock 摘要无效")
	}
	return response, nil
}

func (r HostRepository) call(ctx context.Context, operation string, request, response any) error {
	if r.Host == nil || r.Context == nil || r.Context.GetTenantId() == "" {
		return errors.New("Composition Planner 缺少可信仓库调用上下文")
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return err
	}
	result, raw, err := r.Host.Call(ctx, &contractv1.CallTarget{ExtensionPoint: extpoint.ToolPackage, Capability: repositoryCapability, Operation: &operation}, &contractv1.CallContext{TenantId: r.Context.GetTenantId()}, payload)
	if err != nil {
		return fmt.Errorf("调用制品仓库 %s: %w", operation, err)
	}
	if result == nil || result.GetStatus() != contractv1.CallResult_STATUS_OK {
		return fmt.Errorf("制品仓库拒绝 %s", operation)
	}
	if err := json.Unmarshal(raw, response); err != nil {
		return fmt.Errorf("解析制品仓库 %s 响应: %w", operation, err)
	}
	return nil
}
