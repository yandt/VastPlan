package planner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/extpoint"
	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

const (
	PluginID      = "cn.vastplan.platform.infrastructure.composition-planner"
	PluginVersion = "0.2.1"
	CallerID      = "cn.vastplan.platform.infrastructure.deployment-manager"
)

func Contribution(service *Service) sdk.Contribution {
	return sdk.Contribution{
		ExtensionPoint: extpoint.ToolPackage,
		ID:             backendcompositionv1.PlanningCapability,
		Descriptor:     []byte(`{"title":"应用组合规划器","subcommands":[{"name":"plan","description":"把 Application Intent 编译为可解释的只读组合方案","paramsSchema":{"type":"object","additionalProperties":false,"required":["intent","platformProfile"],"properties":{"intent":{"type":"object"},"platformProfile":{"type":"object"},"configurationSnapshot":{"type":"object"}}},"resultSchema":{"type":"object"}}]}`),
		Handlers: map[string]sdk.Handler{backendcompositionv1.PlanningOperation: func(ctx context.Context, host sdk.Host, callCtx *contractv1.CallContext, raw []byte) (*contractv1.CallResult, []byte, error) {
			if callCtx == nil || callCtx.GetTenantId() == "" || callCtx.GetCaller().GetKind() != contractv1.CallerKind_CALLER_KIND_PLUGIN || callCtx.GetCaller().GetId() != CallerID {
				return nil, nil, errors.New("Composition Planner 只接受可信 Deployment Manager 调用")
			}
			var request backendcompositionv1.PlanningRequest
			if err := decodeStrict(raw, &request); err != nil {
				return nil, nil, err
			}
			if request.Intent.Metadata.Tenant != callCtx.GetTenantId() {
				return nil, nil, errors.New("Application Intent tenant 与可信调用上下文不一致")
			}
			report, err := service.Plan(ctx, HostRepository{Host: host, Context: callCtx}, request)
			if err != nil {
				return nil, nil, err
			}
			payload, err := json.Marshal(report)
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, payload, err
		}},
	}
}

func decodeStrict(raw []byte, output any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(output); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("Composition Planner 请求只能包含一个 JSON 文档")
	}
	return nil
}
