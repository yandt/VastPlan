// Backend 内核（MVP 骨架）。
//
// 内核只提供最小骨架（系统架构 §1.4）：扩展点注册表 + 协议总线 + 生命周期。
// 不含业务——业务一律下沉为插件。
//
// 本 MVP 跑通最小闭环：声明扩展点 → 拉起插件 → 握手/engines 校验 → 贡献注册
// → 激活 → 调用（含插件回调宿主）→ 摘除。
// 尚未实现：节点代理 reconcile、内置插件服务、寻址层、NATS 控制面（见 docs 待决）。
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	contractv1 "github.com/yandt/VastPlan/shared/go/contract/v1"
	"github.com/yandt/VastPlan/shared/go/protocolbus"
	"github.com/yandt/VastPlan/shared/go/registry"
)

// KernelName 本内核的规范 ID（ADR-0015）。
const KernelName = "backend"

// version 由构建时注入：-ldflags "-X main.version=$(cat kernels/backend/VERSION)"
// 单一真源是 kernels/backend/VERSION（ADR-0017 §1）；devel 仅用于未经构建脚本的本地跑。
var version = "0.0.0-devel"

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "用法: %s <插件可执行文件路径>...\n", filepath.Base(os.Args[0]))
		os.Exit(2)
	}
	pluginBins := os.Args[1:]

	logf := func(format string, args ...any) { log.Printf("[kernel] "+format, args...) }
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	logf("内核 %s@%s 启动", KernelName, version)

	// ── 1. 声明扩展点（系统架构 §1.5；契约见第四章）──────────
	reg := registry.New()
	for _, p := range []registry.ExtensionPoint{
		{Name: "tool.package", Dispatch: registry.DispatchSingle},
		{Name: "agent", Dispatch: registry.DispatchSingle},
		{Name: "api.route", Dispatch: registry.DispatchSingle},
		{Name: "permission.checker", Dispatch: registry.DispatchSelect},
		{Name: "event.sink", Dispatch: registry.DispatchFanout},
		{Name: "hook", Dispatch: registry.DispatchFanout},
		{Name: "runner.capability", Dispatch: registry.DispatchSingle},
		{Name: "kernel.service", Dispatch: registry.DispatchSingle}, // 内核自身能力
	} {
		reg.DefinePoint(p)
	}
	logf("已声明 %d 个扩展点", len(reg.Points()))

	// ── 2. 起宿主（gRPC 服务端，插件回连它）+ 登记内核能力 ──
	host := protocolbus.NewHost(KernelName, version, reg, logf)
	if err := host.Start(); err != nil {
		log.Fatalf("[kernel] 宿主启动失败: %v", err)
	}
	defer host.Stop()

	// 内核自身提供的能力：插件可用与调用别的插件完全相同的方式（capability 寻址）回调它
	if err := host.RegisterHostService("kernel.service", "kernel.info", kernelInfo); err != nil {
		log.Fatalf("[kernel] 登记内核能力失败: %v", err)
	}
	logf("已登记内核能力 kernel.info")

	// ── 3. 装载插件：握手 → engines 校验 → 贡献注册 → 激活 ──
	// 权限校验器由插件提供（ADR-0001 一切功能皆插件）；没装它则所有调用被
	// fail-closed 拒绝（ADR-0021）——这正是要演示的。
	for _, bin := range pluginBins {
		if _, err := host.Launch(ctx, bin); err != nil {
			log.Fatalf("[kernel] 装载插件 %s 失败: %v", filepath.Base(bin), err)
		}
	}

	// ── 4. 调用插件贡献的能力（契约全程透传）─────────────────
	callCtx := &contractv1.CallContext{
		Principal: &contractv1.Principal{
			UserId: "u-1001", Username: "zhanghui", TenantId: "acme", IsAdmin: true,
		},
		Caller:   &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_AGENT, Id: "agent-42"},
		Scene:    "agent.tool_call", // 三元组之 scene
		TenantId: "acme",
		Trace:    &contractv1.Trace{TraceId: "trace-abc123", SpanId: "span-1"},
	}

	for _, tc := range []struct{ op, payload string }{
		{"greet", `{"name":"VastPlan"}`},
		{"echo", `{"text":"契约与协议跑通了"}`},
		{"whoami", `{}`},         // 插件回调宿主取内核信息
		{"greet", `{"name":""}`}, // 应用层错误
		{"nope", `{}`},           // 未实现操作
	} {
		op := tc.op
		target := &contractv1.CallTarget{
			ExtensionPoint: "tool.package",
			Capability:     "vastplan.hello", // 四处同名：清单 id = 注册名 = capability
			Operation:      &op,
		}
		resp, err := host.Invoke(ctx, target, callCtx, []byte(tc.payload))
		if err != nil {
			logf("调用 %s 传输层失败: %v", op, err) // 传输层错误与应用层错误严格区分
			continue
		}
		if resp.Result.Status == contractv1.CallResult_STATUS_OK {
			logf("调用 %s → OK (%dms) %s", op, resp.Result.Usage.DurationMs, pretty(resp.Payload))
		} else {
			logf("调用 %s → 应用层错误 code=%s retryable=%v msg=%s",
				op, resp.Result.Error.Code, resp.Result.Error.Retryable, resp.Result.Error.Message)
		}
	}

	// ── 5. 未注册能力的解析应失败（fail-closed）──────────────
	if _, err := host.Invoke(ctx, &contractv1.CallTarget{
		ExtensionPoint: "tool.package", Capability: "not.registered",
	}, callCtx, nil); err != nil {
		logf("未注册能力被正确拒绝: %v", err)
	}

	// ── 6. fanout：发布事件，扇出给所有订阅者 ────────────────
	outcomes := host.PublishEvent(ctx, &contractv1.CallEvent{
		Id: "evt-1", Type: "task.completed", Source: "kernel",
		TenantId: "acme", OccurredAtUnixMs: time.Now().UnixMilli(),
		Trace:   callCtx.Trace,
		Payload: []byte(`{"taskId":"t-1"}`),
	})
	for _, o := range outcomes {
		if o.Err != nil {
			logf("事件汇 %s 失败: %v", o.SinkID, o.Err)
		} else {
			logf("事件已投递给 %s", o.SinkID)
		}
	}
	// 未被任何 sink 订阅的类型：不应投递
	if n := len(host.PublishEvent(ctx, &contractv1.CallEvent{
		Id: "evt-2", Type: "unsubscribed.type", Source: "kernel", TenantId: "acme",
	})); n == 0 {
		logf("未订阅的事件类型无人接收（符合预期）")
	}

	// 向审计插件查账：验证事件**真的送达了插件**，而非宿主自说自话
	if resp, err := host.Invoke(ctx, &contractv1.CallTarget{
		ExtensionPoint: "tool.package", Capability: "demo.audit", Operation: ptr("list"),
	}, callCtx, nil); err == nil && resp.Result.Status == contractv1.CallResult_STATUS_OK {
		logf("审计插件账本 → %s", pretty(resp.Payload))
	}

	logf("MVP 闭环完成")
}

func ptr(s string) *string { return &s }

// kernelInfo 内核自身的能力：回报内核身份——供插件验证它连上的是谁。
func kernelInfo(ctx context.Context, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
	out, _ := json.Marshal(map[string]any{
		"kernel":  KernelName,
		"version": version,
		// 回显调用方，证明 CallContext 在"插件→宿主"方向同样透传
		"callerKind": callCtx.Caller.Kind.String(),
		"tenant":     callCtx.TenantId,
	})
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, out, nil
}

func pretty(b []byte) string {
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return string(b)
	}
	out, _ := json.Marshal(v)
	return string(out)
}
