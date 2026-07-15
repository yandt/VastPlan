// Backend 内核（MVP 骨架）。
//
// 内核只提供最小骨架（系统架构 §1.4）：扩展点注册表 + 协议总线 + 生命周期。
// 不含业务——业务一律下沉为插件。
//
// 本 MVP 跑通最小闭环：定义扩展点 → 拉起插件进程 → 握手 → 贡献注册 → 调用 → 摘除。
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
		fmt.Fprintf(os.Stderr, "用法: %s <插件可执行文件路径>\n", filepath.Base(os.Args[0]))
		os.Exit(2)
	}
	pluginBin := os.Args[1]

	logf := func(format string, args ...any) { log.Printf("[kernel] "+format, args...) }
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	logf("内核 %s@%s 启动", KernelName, version)

	// ── 1. 内核声明扩展点（系统架构 §1.5；契约见第四章）────────
	reg := registry.New()
	for _, p := range []registry.ExtensionPoint{
		{Name: "tool.package", Dispatch: registry.DispatchSingle},
		{Name: "agent", Dispatch: registry.DispatchSingle},
		{Name: "api.route", Dispatch: registry.DispatchSingle},
		{Name: "permission.checker", Dispatch: registry.DispatchSelect},
		{Name: "event.sink", Dispatch: registry.DispatchFanout},
		{Name: "hook", Dispatch: registry.DispatchFanout},
		{Name: "runner.capability", Dispatch: registry.DispatchSingle},
	} {
		reg.DefinePoint(p)
	}
	logf("已声明 %d 个扩展点", len(reg.Points()))

	// ── 2. 拉起插件：握手 → 贡献注册 → 激活 ──────────────────
	host := protocolbus.NewHost(KernelName, version, reg, logf)
	p, err := host.Launch(ctx, pluginBin)
	if err != nil {
		log.Fatalf("[kernel] 装载插件失败: %v", err)
	}
	defer host.Close(p)

	// ── 3. 调用插件贡献的能力（契约全程透传）──────────────────
	callCtx := &contractv1.CallContext{
		Principal: &contractv1.Principal{
			UserId: "u-1001", Username: "zhanghui", TenantId: "acme", IsAdmin: true,
		},
		Caller:   &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_AGENT, Id: "agent-42"},
		Scene:    "agent.tool_call", // 三元组之 scene
		TenantId: "acme",
		Trace:    &contractv1.Trace{TraceId: "trace-abc123", SpanId: "span-1"},
	}

	for _, tc := range []struct {
		op      string
		payload string
	}{
		{"greet", `{"name":"VastPlan"}`},
		{"echo", `{"text":"契约与协议跑通了"}`},
		{"greet", `{"name":""}`},        // 触发应用层错误
		{"nope", `{}`},                  // 触发未实现操作
	} {
		op := tc.op
		target := &contractv1.CallTarget{
			ExtensionPoint: "tool.package",
			Capability:     "vastplan.hello", // 四处同名：清单 id = 注册名 = capability
			Operation:      &op,
		}
		resp, err := host.Invoke(ctx, p, target, callCtx, []byte(tc.payload))
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

	// ── 4. 未注册能力的解析应失败（fail-closed）────────────────
	unknown := &contractv1.CallTarget{ExtensionPoint: "tool.package", Capability: "not.registered"}
	if _, err := host.Invoke(ctx, p, unknown, callCtx, nil); err != nil {
		logf("未注册能力被正确拒绝: %v", err)
	}

	logf("MVP 闭环完成")
}

func pretty(b []byte) string {
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return string(b)
	}
	out, _ := json.Marshal(v)
	return string(out)
}
