package main

import (
	"context"
	"encoding/json"
	"fmt"

	sdk "cdsoft.com.cn/VastPlan/sdk/go/plugin"
	contractv1 "cdsoft.com.cn/VastPlan/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/shared/go/extpoint"
	"cdsoft.com.cn/VastPlan/shared/go/pluginid"
)

const settingsCapability = "platform.settings"

var settingsReadOperations = map[string]struct{}{
	"get": {}, "list": {}, "changesSince": {},
}

func checkerDescriptor(title string) []byte {
	raw, err := json.Marshal(extpoint.CheckerDescriptor{
		Title: title, Applies: &extpoint.Applies{Target: settingsCapability},
	})
	if err != nil {
		panic(err) // 常量 descriptor 无法序列化属于开发期错误。
	}
	return raw
}

// writeGuard 是高优先级安全护栏：除 system 和直接登录的管理员用户外，
// 未登记为只读的操作一律拒绝。插件不能借管理员调用链继承系统设置写权限。
func writeGuard(_ context.Context, _ sdk.Host, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
	request, err := permissionRequest(payload)
	if err != nil {
		return nil, nil, err
	}
	decision := evaluateWriteGuard(callCtx, request)
	return permissionDecision(decision)
}

// baseline 是最低优先级兜底：更高优先级的正式策略可以收紧或扩展读取授权，
// 但没有任何动态设置时仍能让系统与基础层插件完成安全自举。
func baseline(_ context.Context, _ sdk.Host, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
	request, err := permissionRequest(payload)
	if err != nil {
		return nil, nil, err
	}
	decision := evaluateBaseline(callCtx, request)
	return permissionDecision(decision)
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
	if request.Capability != settingsCapability {
		return decision(extpoint.DecisionAbstain, "非系统设置能力")
	}
	if privilegedSettingsWriter(callCtx) || isSettingsRead(request.Operation) {
		return decision(extpoint.DecisionAbstain, "交给后续策略")
	}
	return decision(extpoint.DecisionDeny, "系统设置写操作只允许 system 或直接登录的管理员用户")
}

func evaluateBaseline(callCtx *contractv1.CallContext, request extpoint.PermissionRequest) extpoint.PermissionResponse {
	if request.Capability != settingsCapability {
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
	return sdk.OK(0), raw, nil
}
