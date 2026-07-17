// Package bootstrappolicy 提供自举权限基线的运行时无关实现。进程插件入口和
// dynamic-go 模块都只做适配，策略逻辑因此保持单一真源。
package bootstrappolicy

import (
	"context"
	"encoding/json"
	"fmt"

	contractv1 "cdsoft.com.cn/VastPlan/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/shared/go/extpoint"
	"cdsoft.com.cn/VastPlan/shared/go/pluginid"
)

const (
	PluginID           = "com.vastplan.foundation.security.bootstrap-policy"
	PluginVersion      = "0.1.0"
	WriteGuardID       = "foundation.security.bootstrap-policy.write-guard"
	BaselineID         = "foundation.security.bootstrap-policy.baseline"
	SettingsCapability = "platform.settings"
)

var settingsReadOperations = map[string]struct{}{
	"get": {}, "list": {}, "changesSince": {},
}

func CheckerDescriptor(title string) []byte {
	raw, err := json.Marshal(extpoint.CheckerDescriptor{
		Title: title, Applies: &extpoint.Applies{Target: SettingsCapability},
	})
	if err != nil {
		panic(err)
	}
	return raw
}

func WriteGuard(_ context.Context, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
	request, err := permissionRequest(payload)
	if err != nil {
		return nil, nil, err
	}
	return permissionDecision(evaluateWriteGuard(callCtx, request))
}

func Baseline(_ context.Context, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
	request, err := permissionRequest(payload)
	if err != nil {
		return nil, nil, err
	}
	return permissionDecision(evaluateBaseline(callCtx, request))
}

func permissionRequest(payload []byte) (extpoint.PermissionRequest, error) {
	var request extpoint.PermissionRequest
	if err := json.Unmarshal(payload, &request); err != nil {
		return request, fmt.Errorf("解析权限请求: %w", err)
	}
	if request.Capability == "" {
		return request, fmt.Errorf("权限请求 capability 不能为空")
	}
	return request, nil
}

func evaluateWriteGuard(callCtx *contractv1.CallContext, request extpoint.PermissionRequest) extpoint.PermissionResponse {
	if request.Capability != SettingsCapability {
		return decision(extpoint.DecisionAbstain, "非系统设置能力")
	}
	if privilegedSettingsWriter(callCtx) || isSettingsRead(request.Operation) {
		return decision(extpoint.DecisionAbstain, "交给后续策略")
	}
	return decision(extpoint.DecisionDeny, "系统设置写操作只允许 system 或直接登录的管理员用户")
}

func evaluateBaseline(callCtx *contractv1.CallContext, request extpoint.PermissionRequest) extpoint.PermissionResponse {
	if request.Capability != SettingsCapability {
		return decision(extpoint.DecisionAbstain, "非系统设置能力")
	}
	if privilegedSettingsWriter(callCtx) {
		return decision(extpoint.DecisionAllow, "系统自举身份允许访问系统设置")
	}
	if isSettingsRead(request.Operation) && firstPartyBootstrapReader(callCtx) {
		return decision(extpoint.DecisionAllow, "首方 foundation/platform 插件允许只读访问系统设置")
	}
	return decision(extpoint.DecisionDeny, "自举基线默认拒绝该系统设置访问")
}

func privilegedSettingsWriter(callCtx *contractv1.CallContext) bool {
	if callCtx == nil || callCtx.Caller == nil {
		return false
	}
	switch callCtx.Caller.Kind {
	case contractv1.CallerKind_CALLER_KIND_SYSTEM:
		return true
	case contractv1.CallerKind_CALLER_KIND_USER:
		return callCtx.Principal != nil && callCtx.Principal.IsAdmin
	default:
		return false
	}
}

func firstPartyBootstrapReader(callCtx *contractv1.CallContext) bool {
	if callCtx == nil || callCtx.Caller == nil || callCtx.Caller.Kind != contractv1.CallerKind_CALLER_KIND_PLUGIN {
		return false
	}
	namespace, err := pluginid.ParseFirstParty(callCtx.Caller.Id)
	return err == nil && namespace.IsPlatformBootstrapReader()
}

func isSettingsRead(operation string) bool {
	_, ok := settingsReadOperations[operation]
	return ok
}

func decision(value extpoint.Decision, reason string) extpoint.PermissionResponse {
	return extpoint.PermissionResponse{Decision: value, Reason: reason}
}

func permissionDecision(value extpoint.PermissionResponse) (*contractv1.CallResult, []byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, nil, err
	}
	return &contractv1.CallResult{
		Status: contractv1.CallResult_STATUS_OK,
		Usage:  &contractv1.Usage{},
	}, raw, nil
}
