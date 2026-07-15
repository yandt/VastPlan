// hello-world 插件的 backend 面。
//
// 它只做两件事：声明贡献（一个 tool.package）、实现处理器。
// 协议细节（握手/声明/生命周期/地址回报）由 sdk/go/plugin 承担。
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	sdk "github.com/yandt/VastPlan/sdk/go/plugin"
	contractv1 "github.com/yandt/VastPlan/shared/go/contract/v1"
)

// descriptor 对应清单 contributes.backend.tools[0]，与 vastplan.plugin.json 保持一致。
// 扩展点 tool.package 的贡献契约见《插件契约与协议》第四章 §4.3。
const descriptor = `{
  "title": "Hello 工具包",
  "service_role": "backend",
  "subcommands": [
    {"name": "greet", "description": "向指定对象问好"},
    {"name": "echo",  "description": "原样回显"}
  ]
}`

func main() {
	// id/version/engines 与 vastplan.plugin.json 保持一致（清单是单一真源，ADR-0017 §1）
	p := sdk.New("com.vastplan.hello-world", "0.1.0", map[string]string{
		"backend": "^0.1", // 只贡献 backend 面；宿主据此校验内核版本
	})

	p.Contribute(sdk.Contribution{
		ExtensionPoint: "tool.package",
		ID:             "vastplan.hello", // = 清单 id = CallTarget.capability
		Descriptor:     []byte(descriptor),
		Handlers: map[string]sdk.Handler{
			"greet": greet,
			"echo":  echo,
		},
	})

	if err := p.Serve(); err != nil {
		log.Fatalf("插件退出: %v", err)
	}
}

// greet 演示：读 CallContext 里透传的身份/租户/场景，返回问候。
func greet(ctx context.Context, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
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
	greeting := fmt.Sprintf("你好，%s！我是插件 com.vastplan.hello-world。", in.Name)
	out, _ := json.Marshal(map[string]any{
		"greeting":   greeting,
		"calledBy":   callCtx.Principal.Username,
		"tenant":     callCtx.TenantId,
		"scene":      callCtx.Scene,
		"traceId":    callCtx.Trace.TraceId,
		"callerKind": callCtx.Caller.Kind.String(),
	})
	return sdk.OK(time.Since(start).Milliseconds()), out, nil
}

func echo(ctx context.Context, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
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
