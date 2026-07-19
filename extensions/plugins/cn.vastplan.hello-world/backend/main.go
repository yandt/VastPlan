// hello-world 插件的 backend 面。
//
// 它只做两件事：声明贡献（一个 tool.package）、实现处理器。
// 协议细节（回连/握手/声明/生命周期/心跳/双向流）由 extensions/sdk/go/plugin 承担。
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

// descriptor 对应清单 contributes.backend.tools[0]，与 vastplan.plugin.json 保持一致。
// 扩展点 tool.package 的贡献契约见《插件契约与协议》第四章 §4.3。
const descriptor = `{
  "title": "Hello 工具包",
  "subcommands": [
    {"name": "greet", "description": "向指定对象问好", "paramsSchema": {"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}},
    {"name": "echo", "description": "原样回显", "paramsSchema": {"type":"object","properties":{"text":{"type":"string"}},"required":["text"]}},
    {"name": "whoami", "description": "回调宿主取内核信息"}
  ]
}`

func main() {
	// id/version/engines 与 vastplan.plugin.json 保持一致（清单是单一真源，ADR-0017 §1）
	p := sdk.New("cn.vastplan.hello-world", "0.1.0", map[string]string{
		"backend": "^0.1 || ^1.0", // 只贡献 backend 面；已通过 Backend 0.1/1.0 兼容门禁
	})

	p.Contribute(sdk.Contribution{
		ExtensionPoint: "tool.package",
		ID:             "vastplan.hello", // = 清单 id = CallTarget.capability
		Descriptor:     []byte(descriptor),
		Handlers: map[string]sdk.Handler{
			"greet":  greet,
			"echo":   echo,
			"whoami": whoami,
		},
	})

	if err := p.Serve(); err != nil {
		log.Fatalf("插件退出: %v", err)
	}
}

// greet 演示：读 CallContext 里透传的身份/租户/场景，返回问候。
func greet(ctx context.Context, host sdk.Host, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
	start := time.Now()

	var in struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(payload, &in); err != nil {
		return nil, nil, fmt.Errorf("参数解析失败: %w", err)
	}
	if in.Name == "" {
		return nil, nil, fmt.Errorf("参数 name 不能为空")
	}

	// 上下文全程透传：插件能看到调用方是谁、在哪个租户、什么场景
	out, _ := json.Marshal(map[string]any{
		"greeting":   fmt.Sprintf("你好，%s！我是插件 cn.vastplan.hello-world。", in.Name),
		"calledBy":   callCtx.Principal.Username,
		"tenant":     callCtx.TenantId,
		"scene":      callCtx.Scene,
		"traceId":    callCtx.Trace.TraceId,
		"callerKind": callCtx.Caller.Kind.String(),
	})
	return sdk.OK(time.Since(start).Milliseconds()), out, nil
}

func echo(ctx context.Context, host sdk.Host, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
	start := time.Now()
	var in struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(payload, &in); err != nil {
		return nil, nil, fmt.Errorf("参数解析失败: %w", err)
	}
	out, _ := json.Marshal(map[string]any{"echo": in.Text})
	return sdk.OK(time.Since(start).Milliseconds()), out, nil
}

// whoami 演示**插件回调宿主**：按 capability 名寻址内核能力（§2.4）。
// 插件不知道也不需要知道 kernel.info 由谁实现——这正是低耦合的体现。
func whoami(ctx context.Context, host sdk.Host, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
	start := time.Now()

	op := "get"
	res, info, err := host.Call(ctx, &contractv1.CallTarget{
		ExtensionPoint: "kernel.service",
		Capability:     "kernel.info",
		Operation:      &op,
	}, callCtx, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("回调宿主失败: %w", err)
	}
	if res.Status != contractv1.CallResult_STATUS_OK {
		return nil, nil, fmt.Errorf("宿主返回错误: %s", res.Error.Message)
	}

	var kernel map[string]any
	if err := json.Unmarshal(info, &kernel); err != nil {
		return nil, nil, fmt.Errorf("解析内核信息失败: %w", err)
	}
	out, _ := json.Marshal(map[string]any{
		"plugin":       "cn.vastplan.hello-world@0.1.0",
		"hostReported": kernel,
	})
	return sdk.OK(time.Since(start).Milliseconds()), out, nil
}
