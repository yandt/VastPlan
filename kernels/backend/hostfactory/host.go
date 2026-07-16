// Package hostfactory 集中声明 backend 内核的扩展点和内置能力。
//
// 手工演示入口与 Node Agent 自动装配必须使用同一宿主工厂；否则两条启动路径会
// 悄悄形成不同的内核能力集合。
package hostfactory

import (
	"context"
	"encoding/json"

	contractv1 "cdsoft.com.cn/VastPlan/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/shared/go/extpoint"
	"cdsoft.com.cn/VastPlan/shared/go/protocolbus"
	"cdsoft.com.cn/VastPlan/shared/go/registry"
)

// KernelName 是 backend 内核规范 ID。
const KernelName = "backend"

// New 创建尚未 Start 的 backend 插件宿主。
func New(version string, logf func(string, ...any)) (*protocolbus.Host, error) {
	reg := registry.New()
	for _, point := range []registry.ExtensionPoint{
		{Name: extpoint.ToolPackage, Dispatch: registry.DispatchSingle},
		{Name: extpoint.Agent, Dispatch: registry.DispatchSingle},
		{Name: extpoint.APIRoute, Dispatch: registry.DispatchSingle},
		{Name: extpoint.PermissionChecker, Dispatch: registry.DispatchSelect},
		{Name: extpoint.EventSink, Dispatch: registry.DispatchFanout},
		{Name: extpoint.Hook, Dispatch: registry.DispatchFanout},
		{Name: extpoint.RunnerCapability, Dispatch: registry.DispatchSingle},
		{Name: extpoint.KernelService, Dispatch: registry.DispatchSingle},
	} {
		reg.DefinePoint(point)
	}
	host := protocolbus.NewHost(KernelName, version, reg, logf)
	if err := host.RegisterHostService(extpoint.KernelService, "kernel.info", kernelInfo(version)); err != nil {
		return nil, err
	}
	return host, nil
}

func kernelInfo(version string) protocolbus.HostService {
	return func(_ context.Context, callCtx *contractv1.CallContext, _ []byte) (*contractv1.CallResult, []byte, error) {
		out, _ := json.Marshal(map[string]any{
			"kernel":     KernelName,
			"version":    version,
			"callerKind": callCtx.GetCaller().GetKind().String(),
			"tenant":     callCtx.GetTenantId(),
		})
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, out, nil
	}
}
