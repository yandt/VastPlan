// Package policy implements the coarse permission boundary for interaction
// broker calls. Object-level eligibility remains exclusively in the Broker.
package policy

import (
	"context"
	"encoding/json"

	contractv1 "cdsoft.com.cn/VastPlan/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/shared/go/extpoint"
	"cdsoft.com.cn/VastPlan/shared/go/interactionapi"
)

const (
	PluginID      = "com.vastplan.foundation.security.interaction-access-policy"
	PluginVersion = "0.1.0"
	Capability    = "foundation.security.interaction-access-policy"
)

func Check(_ context.Context, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
	var request extpoint.PermissionRequest
	if err := json.Unmarshal(payload, &request); err != nil {
		return nil, nil, err
	}
	decision, reason := decide(callCtx, request)
	raw, err := json.Marshal(extpoint.PermissionResponse{Decision: decision, Reason: reason})
	if err != nil {
		return nil, nil, err
	}
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
}

func decide(callCtx *contractv1.CallContext, request extpoint.PermissionRequest) (extpoint.Decision, string) {
	if callCtx == nil || callCtx.Caller == nil || callCtx.Caller.Id == "" || callCtx.TenantId == "" {
		return extpoint.DecisionDeny, "缺少经验证调用身份或租户"
	}
	if request.Capability == "kernel.config.get" && callCtx.Caller.Kind == contractv1.CallerKind_CALLER_KIND_PLUGIN && callCtx.Caller.Id == BrokerPluginID() {
		return extpoint.DecisionAllow, "Broker 受限宿主回调"
	}
	if request.Capability != interactionapi.Capability {
		return extpoint.DecisionAbstain, "非交互能力"
	}
	switch request.Operation {
	case "open", "watch", "cancel":
		switch callCtx.Caller.Kind {
		case contractv1.CallerKind_CALLER_KIND_PLUGIN, contractv1.CallerKind_CALLER_KIND_RUNNER, contractv1.CallerKind_CALLER_KIND_SYSTEM:
			return extpoint.DecisionAllow, "受信来源可管理交互"
		default:
			return extpoint.DecisionDeny, "仅受信来源可创建或取消交互"
		}
	case "list", "get", "present", "respond":
		if callCtx.Caller.Kind != contractv1.CallerKind_CALLER_KIND_USER {
			return extpoint.DecisionDeny, "仅已认证用户可呈现或响应交互"
		}
		return extpoint.DecisionAllow, "Broker 继续执行对象级资格校验"
	default:
		return extpoint.DecisionDeny, "未知交互操作"
	}
}

func BrokerPluginID() string { return "com.vastplan.platform.interaction.broker" }
