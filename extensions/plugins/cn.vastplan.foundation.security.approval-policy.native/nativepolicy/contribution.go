package nativepolicy

import (
	"context"
	"encoding/json"
	"errors"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/extpoint"
	approvalv2 "cdsoft.com.cn/VastPlan/contracts/schemas/approval/v2"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func (p *Provider) Contribution() sdk.Contribution {
	return sdk.Contribution{ExtensionPoint: extpoint.ToolPackage, ID: approvalv2.Capability, Descriptor: descriptor(), Handlers: map[string]sdk.Handler{
		"evaluate":      p.handleEvaluate,
		"evaluateBatch": p.handleEvaluateBatch,
		"health":        p.handleHealth,
	}}
}

func (p *Provider) handleEvaluate(_ context.Context, _ sdk.Host, call *contractv1.CallContext, raw []byte) (*contractv1.CallResult, []byte, error) {
	var request approvalv2.EvaluateRequest
	if err := approvalv2.DecodeStrict(raw, &request); err != nil {
		return providerFailure("approval.provider.invalid_request", err, false), nil, nil
	}
	if err := validateCaller(call, []approvalv2.EvaluationInput{request.Input}); err != nil {
		return providerFailure("approval.provider.forbidden", err, false), nil, nil
	}
	decision, err := p.Evaluate(request)
	if err != nil {
		return providerFailure("approval.provider.rejected", err, false), nil, nil
	}
	encoded, err := json.Marshal(approvalv2.EvaluateResult{Decision: decision})
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, encoded, err
}

func (p *Provider) handleEvaluateBatch(_ context.Context, _ sdk.Host, call *contractv1.CallContext, raw []byte) (*contractv1.CallResult, []byte, error) {
	var request approvalv2.EvaluateBatchRequest
	if err := approvalv2.DecodeStrict(raw, &request); err != nil {
		return providerFailure("approval.provider.invalid_request", err, false), nil, nil
	}
	if err := validateCaller(call, request.Inputs); err != nil {
		return providerFailure("approval.provider.forbidden", err, false), nil, nil
	}
	decisions, err := p.EvaluateBatch(request)
	if err != nil {
		return providerFailure("approval.provider.rejected", err, false), nil, nil
	}
	encoded, err := json.Marshal(approvalv2.EvaluateBatchResult{Decisions: decisions})
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, encoded, err
}

func (p *Provider) handleHealth(_ context.Context, _ sdk.Host, call *contractv1.CallContext, _ []byte) (*contractv1.CallResult, []byte, error) {
	if call == nil || call.Caller == nil || (call.Caller.Kind != contractv1.CallerKind_CALLER_KIND_PLUGIN && call.Caller.Kind != contractv1.CallerKind_CALLER_KIND_SYSTEM) {
		return providerFailure("approval.provider.forbidden", errors.New("Provider health 只接受插件或系统调用"), false), nil, nil
	}
	encoded, err := json.Marshal(p.Health())
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, encoded, err
}

func validateCaller(call *contractv1.CallContext, inputs []approvalv2.EvaluationInput) error {
	if call == nil || call.Caller == nil || call.Caller.Kind != contractv1.CallerKind_CALLER_KIND_PLUGIN || call.Caller.Id == "" || call.Principal == nil || call.Principal.UserId == "" || call.TenantId == "" {
		return errors.New("Approval Provider 只接受保留可信 Principal 的插件调用")
	}
	for _, input := range inputs {
		if input.Actor.ID != call.Principal.UserId || input.TenantID != call.TenantId {
			return errors.New("Approval facts 与可信 CallContext 主体或租户不一致")
		}
	}
	return nil
}

func descriptor() []byte {
	raw, _ := json.Marshal(map[string]any{"title": "VastPlan Native Approval Policy Provider", "subcommands": []map[string]string{
		{"name": "evaluate", "description": "按精确 Profile 求值一个审批请求"},
		{"name": "evaluateBatch", "description": "按精确 Profile 批量求值审批读模型"},
		{"name": "health", "description": "读取 Provider 与 Profile 健康状态"},
	}})
	return raw
}

func providerFailure(code string, err error, retryable bool) *contractv1.CallResult {
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: code, Message: err.Error(), Retryable: retryable}}
}
