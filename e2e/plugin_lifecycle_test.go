//go:build e2e

// 内核 ↔ 插件 的跨进程真实链路 E2E（ADR-0018）。
//
// 走真实 proto/协议/进程，不 mock 契约——否则测不出协议漂移与版本失配。
// 运行：go test -tags=e2e ./e2e/...
package e2e

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	contractv1 "github.com/yandt/VastPlan/shared/go/contract/v1"
	"github.com/yandt/VastPlan/shared/go/protocolbus"
	"github.com/yandt/VastPlan/shared/go/registry"
)

const kernelName = "backend"

// buildPlugin 把插件源码构建成二进制，返回路径。
func buildPlugin(t *testing.T, pkgDir string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "plugin-under-test")
	cmd := exec.Command("go", "build", "-o", bin, pkgDir)
	cmd.Dir = repoRoot(t)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("构建插件 %s 失败: %v\n%s", pkgDir, err, out)
	}
	return bin
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("取工作目录失败: %v", err)
	}
	return filepath.Dir(wd) // e2e/ 的上一级
}

// newHost 造一个指定内核版本的宿主 + 声明扩展点。
func newHost(t *testing.T, kernelVersion string) *protocolbus.Host {
	t.Helper()
	reg := registry.New()
	for _, p := range []registry.ExtensionPoint{
		{Name: "tool.package", Dispatch: registry.DispatchSingle},
		{Name: "event.sink", Dispatch: registry.DispatchFanout},
	} {
		reg.DefinePoint(p)
	}
	return protocolbus.NewHost(kernelName, kernelVersion, reg, func(string, ...any) {})
}

func testCallContext() *contractv1.CallContext {
	return &contractv1.CallContext{
		Principal: &contractv1.Principal{UserId: "u-1", Username: "tester", TenantId: "acme"},
		Caller:    &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_AGENT, Id: "agent-1"},
		Scene:     "agent.tool_call",
		TenantId:  "acme",
		Trace:     &contractv1.Trace{TraceId: "trace-e2e", SpanId: "span-1"},
	}
}

// 完整生命周期：拉起 → 握手 → engines 校验 → 贡献注册 → 激活 → 调用 → 摘除。
func TestPluginLifecycle_HappyPath(t *testing.T) {
	bin := buildPlugin(t, "./plugins/com.vastplan.hello-world/backend")
	host := newHost(t, "0.1.0") // 满足插件的 engines ^0.1

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	p, err := host.Launch(ctx, bin)
	if err != nil {
		t.Fatalf("装载插件失败: %v", err)
	}
	defer host.Close(p)

	if p.PluginID != "com.vastplan.hello-world" {
		t.Fatalf("插件 id = %q，期望 com.vastplan.hello-world", p.PluginID)
	}
	if p.SessionID == "" {
		t.Fatal("宿主应签发会话票据")
	}

	// 贡献应已接入注册表（四处同名：清单 id = 注册名 = capability）
	if _, ok := host.Registry.Lookup("tool.package", "vastplan.hello"); !ok {
		t.Fatal("贡献 vastplan.hello 应已注册进 tool.package")
	}

	// 调用成功，且 CallContext 全程透传到插件
	op := "greet"
	resp, err := host.Invoke(ctx, p, &contractv1.CallTarget{
		ExtensionPoint: "tool.package", Capability: "vastplan.hello", Operation: &op,
	}, testCallContext(), []byte(`{"name":"E2E"}`))
	if err != nil {
		t.Fatalf("调用 greet 传输层失败: %v", err)
	}
	if resp.Result.Status != contractv1.CallResult_STATUS_OK {
		t.Fatalf("greet 应成功，实际 %v", resp.Result)
	}

	var got map[string]any
	if err := json.Unmarshal(resp.Payload, &got); err != nil {
		t.Fatalf("结果解析失败: %v", err)
	}
	// 契约透传：插件确实看到了调用方/租户/场景/trace
	if got["calledBy"] != "tester" || got["tenant"] != "acme" ||
		got["scene"] != "agent.tool_call" || got["traceId"] != "trace-e2e" {
		t.Fatalf("CallContext 未如实透传到插件，实际: %v", got)
	}

	// 关闭后贡献应被摘除（ADR-0004 故障隔离）
	_ = host.Close(p)
	if _, ok := host.Registry.Lookup("tool.package", "vastplan.hello"); ok {
		t.Fatal("插件关闭后其贡献应被摘除")
	}
}

// 应用层错误须经 CallResult 返回，且与传输层错误严格区分（插件契约与协议 §2.7）。
func TestPluginInvoke_ApplicationErrorsAreNotTransportErrors(t *testing.T) {
	bin := buildPlugin(t, "./plugins/com.vastplan.hello-world/backend")
	host := newHost(t, "0.1.0")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	p, err := host.Launch(ctx, bin)
	if err != nil {
		t.Fatalf("装载插件失败: %v", err)
	}
	defer host.Close(p)

	cases := []struct {
		name      string
		op        string
		payload   string
		wantCode  string
		retryable bool
	}{
		{"参数非法", "greet", `{"name":""}`, "plugin.handler_error", true},
		{"未实现的操作", "nope", `{}`, "capability.not_found", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			op := c.op
			resp, err := host.Invoke(ctx, p, &contractv1.CallTarget{
				ExtensionPoint: "tool.package", Capability: "vastplan.hello", Operation: &op,
			}, testCallContext(), []byte(c.payload))
			// 关键：应用层错误不得表现为传输层错误
			if err != nil {
				t.Fatalf("应用层错误不应冒泡为传输层错误，实际: %v", err)
			}
			if resp.Result.Status != contractv1.CallResult_STATUS_ERROR {
				t.Fatalf("期望 STATUS_ERROR，实际 %v", resp.Result.Status)
			}
			if resp.Result.Error.Code != c.wantCode {
				t.Fatalf("错误码 = %q，期望 %q", resp.Result.Error.Code, c.wantCode)
			}
			if resp.Result.Error.Retryable != c.retryable {
				t.Fatalf("retryable = %v，期望 %v", resp.Result.Error.Retryable, c.retryable)
			}
		})
	}
}

// 未注册能力的解析必须失败（fail-closed）。
func TestPluginInvoke_UnregisteredCapabilityRejected(t *testing.T) {
	bin := buildPlugin(t, "./plugins/com.vastplan.hello-world/backend")
	host := newHost(t, "0.1.0")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	p, err := host.Launch(ctx, bin)
	if err != nil {
		t.Fatalf("装载插件失败: %v", err)
	}
	defer host.Close(p)

	_, err = host.Invoke(ctx, p, &contractv1.CallTarget{
		ExtensionPoint: "tool.package", Capability: "not.registered",
	}, testCallContext(), nil)
	if err == nil {
		t.Fatal("未注册能力应被拒绝，实际通过了")
	}
}

// engines fail-closed：内核版本不满足插件声明范围时，必须拒绝装载
// （ADR-0017 §4 强制点 2）。这是版本机制的核心保障，必须由真实链路验证。
func TestPluginLaunch_IncompatibleKernelVersionRejected(t *testing.T) {
	bin := buildPlugin(t, "./plugins/com.vastplan.hello-world/backend")
	host := newHost(t, "0.2.0") // 插件要求 ^0.1，0.2.0 不满足

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	p, err := host.Launch(ctx, bin)
	if err == nil {
		host.Close(p)
		t.Fatal("内核版本不兼容时应拒绝装载，实际装上了")
	}
	if !strings.Contains(err.Error(), "不满足插件要求") {
		t.Fatalf("错误信息应说明版本不满足，实际: %v", err)
	}
	// 被拒绝的插件不得留下任何贡献
	if got := host.Registry.List("tool.package"); len(got) != 0 {
		t.Fatalf("装载被拒后不应残留贡献，实际 %d 条", len(got))
	}
}

// 夹具插件：未声明本内核 engines → fail-closed 拒绝（ADR-0017 §4）。
func TestPluginLaunch_MissingEnginesRejected(t *testing.T) {
	bin := buildPlugin(t, "./e2e/fixtures/plugins/no-engines")
	host := newHost(t, "0.1.0")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	p, err := host.Launch(ctx, bin)
	if err == nil {
		host.Close(p)
		t.Fatal("未声明本内核 engines 的插件应被拒绝，实际装上了")
	}
	if !strings.Contains(err.Error(), "未声明") {
		t.Fatalf("错误信息应说明未声明 engines，实际: %v", err)
	}
}
