//go:build e2e

// 内核 ↔ 插件 的跨进程真实链路 E2E（ADR-0018）。
//
// 走真实 proto/协议/进程，不 mock 契约——否则测不出协议漂移与版本失配。
// 运行：go test -tags=e2e ./e2e/...
package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cdsoft.com.cn/VastPlan/kernels/backend/nodeagent"
	contractv1 "cdsoft.com.cn/VastPlan/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/shared/go/protocolbus"
	"cdsoft.com.cn/VastPlan/shared/go/registry"
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

// newHost 造一个指定内核版本的宿主：声明扩展点、登记内核能力、开始监听。
func newHost(t *testing.T, kernelVersion string) *protocolbus.Host {
	t.Helper()
	reg := registry.New()
	for _, p := range []registry.ExtensionPoint{
		{Name: "tool.package", Dispatch: registry.DispatchSingle},
		{Name: "permission.checker", Dispatch: registry.DispatchSelect},
		{Name: "event.sink", Dispatch: registry.DispatchFanout},
		{Name: "hook", Dispatch: registry.DispatchFanout},
		{Name: "kernel.service", Dispatch: registry.DispatchSingle},
	} {
		reg.DefinePoint(p)
	}

	// 日志接到 t：否则"贡献被拒"这类关键信息进黑洞，失败时无从排障
	h := protocolbus.NewHost(kernelName, kernelVersion, reg,
		func(format string, args ...any) { t.Logf("[host] "+format, args...) })
	// 缩短时限：让"失联/超时"类用例秒级完成，不必真等生产默认值
	h.LaunchTimeout = 20 * time.Second
	h.CallTimeout = 5 * time.Second
	h.HeartbeatEvery = 200 * time.Millisecond
	h.HeartbeatTimeout = 2 * time.Second
	h.PluginEnvironmentAllowlist = []string{"VASTPLAN_MIGRATION_LOG", "VASTPLAN_MIGRATION_FAIL"}

	if err := h.Start(); err != nil {
		t.Fatalf("宿主启动失败: %v", err)
	}
	t.Cleanup(h.Stop)

	// 内核自身能力：供插件回调（§2.4）
	err := h.RegisterHostService("kernel.service", "kernel.info",
		func(ctx context.Context, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
			out, _ := json.Marshal(map[string]any{
				"kernel": kernelName, "version": kernelVersion,
				"callerKind": callCtx.Caller.Kind.String(), "callerId": callCtx.Caller.Id,
				"tenant": callCtx.TenantId, "traceId": callCtx.GetTrace().GetTraceId(),
			})
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, out, nil
		})
	if err != nil {
		t.Fatalf("登记内核能力失败: %v", err)
	}
	return h
}

// allowAllPermissions 让本测试显式声明"放行一切"。
//
// 供**不测权限**的用例使用：它们测协议/生命周期，但 Invoke 会强制权限判定
// （零校验器 → fail-closed 拒绝，ADR-0021）。与其偷偷跳过校验，不如让每个测试
// 把自己的策略摆到明面上。测权限的用例不要调它——它们装真的权限插件。
func allowAllPermissions(t *testing.T, h *protocolbus.Host) {
	t.Helper()
	err := h.RegisterHostService("permission.checker", "test.allow-all",
		func(ctx context.Context, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK},
				[]byte(`{"decision":"allow","reason":"测试放行策略"}`), nil
		})
	if err != nil {
		t.Fatalf("注册测试放行策略失败: %v", err)
	}
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

func toolTarget(capability, op string) *contractv1.CallTarget {
	return &contractv1.CallTarget{ExtensionPoint: "tool.package", Capability: capability, Operation: &op}
}

func TestFirstPartyReferencePluginsSupportBackend1(t *testing.T) {
	plugins := []string{
		"./plugins/com.vastplan.demo-audit/backend",
		"./plugins/com.vastplan.demo-permission/backend",
		"./plugins/com.vastplan.demo-quota/backend",
		"./plugins/com.vastplan.hello-world/backend",
	}
	for _, pluginPath := range plugins {
		t.Run(filepath.Base(filepath.Dir(filepath.Dir(pluginPath))), func(t *testing.T) {
			bin := buildPlugin(t, pluginPath)
			host := newHost(t, "1.0.0")
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			process, err := host.Launch(ctx, bin)
			if err != nil {
				t.Fatalf("第一方参考插件必须通过 Backend 1.0 engines 握手: %v", err)
			}
			if err := host.Close(process); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// 兼容性夹具绕过当前 sdk/go/plugin，直接使用 Plugin-Host v1 消息集。
// 因此 SDK 内部实现即使重构，也不能掩盖宿主对既有 wire 客户端的破坏。
func TestPluginHost_LegacyV1RawClientRemainsCompatible(t *testing.T) {
	bin := buildPlugin(t, "./e2e/fixtures/plugins/legacy-v1")
	host := newHost(t, "1.0.0")
	allowAllPermissions(t, host)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	process, err := host.Launch(ctx, bin)
	if err != nil {
		t.Fatalf("旧 v1 客户端接入失败: %v", err)
	}

	payload := []byte(`{"wire":"v1"}`)
	response, err := host.Invoke(ctx, toolTarget("fixture.legacy-v1", "echo"), testCallContext(), payload)
	if err != nil {
		t.Fatalf("旧 v1 客户端调用失败: %v", err)
	}
	if response.Result.GetStatus() != contractv1.CallResult_STATUS_OK || string(response.Payload) != string(payload) {
		t.Fatalf("旧 v1 客户端响应漂移: %+v payload=%s", response.Result, response.Payload)
	}
	if err := host.Close(process); err != nil {
		t.Fatalf("关闭旧 v1 客户端失败: %v", err)
	}
}

func TestPluginMigrationLifecycle_ThreePhaseAndMissingHandlerFailClosed(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "migration.log")
	t.Setenv("VASTPLAN_MIGRATION_LOG", logPath)
	bin := buildPlugin(t, "./e2e/fixtures/plugins/migrator")
	host := newHost(t, "0.1.0")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	process, err := host.Launch(ctx, bin)
	if err != nil {
		t.Fatalf("迁移夹具接入失败: %v", err)
	}
	request := protocolbus.MigrationCommand{
		TransactionID: "migration-e2e-1",
		From:          protocolbus.StateIdentity{Format: "com.example.state", FormatVersion: 1},
		To:            protocolbus.StateIdentity{Format: "com.example.state", FormatVersion: 2},
	}
	for _, operation := range []protocolbus.MigrationOperation{
		protocolbus.MigrationPrepare, protocolbus.MigrationCommit, protocolbus.MigrationRollback,
	} {
		request.Operation = operation
		if err := host.Migrate(ctx, process, request); err != nil {
			t.Fatalf("迁移阶段 %s 失败: %v", operation, err)
		}
	}
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	want := "prepare migration-e2e-1 com.example.state@1 com.example.state@2\n" +
		"commit migration-e2e-1 com.example.state@1 com.example.state@2\n" +
		"rollback migration-e2e-1 com.example.state@1 com.example.state@2\n"
	if string(raw) != want {
		t.Fatalf("迁移阶段或字段漂移:\n%s\n期望:\n%s", raw, want)
	}

	if err := host.Close(process); err != nil {
		t.Fatal(err)
	}
	plain, err := host.Launch(ctx, buildPlugin(t, "./plugins/com.vastplan.hello-world/backend"))
	if err != nil {
		t.Fatal(err)
	}
	request.Operation = protocolbus.MigrationPrepare
	if err := host.Migrate(ctx, plain, request); err == nil || !strings.Contains(err.Error(), "未实现") {
		t.Fatalf("未实现迁移处理器必须 fail-closed，实际 err=%v", err)
	}
}

func TestProtocolRuntime_StateMigrationCommitAndFailureRollback(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "runtime-migration.log")
	t.Setenv("VASTPLAN_MIGRATION_LOG", logPath)
	v1Bin := buildPlugin(t, "./e2e/fixtures/plugins/migrator-v1")
	v2Bin := buildPlugin(t, "./e2e/fixtures/plugins/migrator")
	runtime := nodeagent.NewProtocolRuntime("0.1.0", t.Logf)
	t.Cleanup(func() { _ = runtime.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := runtime.Apply(ctx, nodeagent.RuntimeUnit{
		ID: "backend-main", Fingerprint: "v1", ServiceRole: "backend",
		Plugins: []nodeagent.InstalledPlugin{{
			ID: "com.vastplan.fixture.migrator", Version: "1.0.0", EntryPath: v1Bin,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	plan12 := nodeagent.StateMigrationPlan{
		PluginID: "com.vastplan.fixture.migrator", TransactionID: "runtime-v1-v2",
		From: nodeagent.PluginStateIdentity{Format: "com.example.state", FormatVersion: 1},
		To:   nodeagent.PluginStateIdentity{Format: "com.example.state", FormatVersion: 2},
	}
	if err := runtime.Apply(ctx, nodeagent.RuntimeUnit{
		ID: "backend-main", Fingerprint: "v2", ServiceRole: "backend",
		Plugins: []nodeagent.InstalledPlugin{{
			ID: "com.vastplan.fixture.migrator", Version: "2.0.0", EntryPath: v2Bin,
		}},
		Migrations: []nodeagent.StateMigrationPlan{plan12},
	}); err != nil {
		t.Fatalf("状态迁移升级失败: %v", err)
	}
	if !runtime.IsRunning("backend-main", "v2") {
		t.Fatal("迁移成功后候选没有取得运行所有权")
	}

	t.Setenv("VASTPLAN_MIGRATION_FAIL", "commit")
	plan23 := nodeagent.StateMigrationPlan{
		PluginID: "com.vastplan.fixture.migrator", TransactionID: "runtime-v2-v3",
		From: nodeagent.PluginStateIdentity{Format: "com.example.state", FormatVersion: 2},
		To:   nodeagent.PluginStateIdentity{Format: "com.example.state", FormatVersion: 3},
	}
	err := runtime.Apply(ctx, nodeagent.RuntimeUnit{
		ID: "backend-main", Fingerprint: "v3", ServiceRole: "backend",
		Plugins: []nodeagent.InstalledPlugin{{
			ID: "com.vastplan.fixture.migrator", Version: "2.0.0", EntryPath: v2Bin,
		}},
		Migrations: []nodeagent.StateMigrationPlan{plan23},
	})
	if err == nil || !strings.Contains(err.Error(), "commit") {
		t.Fatalf("提交失败必须阻止升级: %v", err)
	}
	if !runtime.IsRunning("backend-main", "v2") || runtime.IsRunning("backend-main", "v3") {
		t.Fatal("迁移失败没有保留旧运行实例")
	}
	t.Setenv("VASTPLAN_MIGRATION_FAIL", "prepare")
	plan23.TransactionID = "runtime-v2-v3-prepare-fails"
	err = runtime.Apply(ctx, nodeagent.RuntimeUnit{
		ID: "backend-main", Fingerprint: "v3-prepare-fails", ServiceRole: "backend",
		Plugins: []nodeagent.InstalledPlugin{{
			ID: "com.vastplan.fixture.migrator", Version: "2.0.0", EntryPath: v2Bin,
		}},
		Migrations: []nodeagent.StateMigrationPlan{plan23},
	})
	if err == nil || !strings.Contains(err.Error(), "prepare") || !runtime.IsRunning("backend-main", "v2") {
		t.Fatalf("PREPARE 部分失败必须回滚并保留旧实例: %v", err)
	}
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	wantLines := []string{
		"prepare runtime-v1-v2 com.example.state@1 com.example.state@2",
		"commit runtime-v1-v2 com.example.state@1 com.example.state@2",
		"prepare runtime-v2-v3 com.example.state@2 com.example.state@3",
		"commit runtime-v2-v3 com.example.state@2 com.example.state@3",
		"rollback runtime-v2-v3 com.example.state@2 com.example.state@3",
		"prepare runtime-v2-v3-prepare-fails com.example.state@2 com.example.state@3",
		"rollback runtime-v2-v3-prepare-fails com.example.state@2 com.example.state@3",
	}
	if got := strings.Split(strings.TrimSpace(string(raw)), "\n"); strings.Join(got, "|") != strings.Join(wantLines, "|") {
		t.Fatalf("Runtime 迁移事务序列错误:\n%s", raw)
	}
}

// 完整生命周期：拉起 → 回连 → 握手 → engines 校验 → 贡献注册 → 激活 → 调用 → 摘除。
func TestPluginLifecycle_HappyPath(t *testing.T) {
	bin := buildPlugin(t, "./plugins/com.vastplan.hello-world/backend")
	host := newHost(t, "0.1.0")  // 满足插件的 engines ^0.1
	allowAllPermissions(t, host) // 本测试不测权限，显式放行

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	p, err := host.Launch(ctx, bin)
	if err != nil {
		t.Fatalf("装载插件失败: %v", err)
	}

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
	resp, err := host.Invoke(ctx, toolTarget("vastplan.hello", "greet"), testCallContext(), []byte(`{"name":"E2E"}`))
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

// 插件回调宿主（§2.4）：经 capability 寻址内核能力，且 CallContext 在反方向同样透传。
// 这是 Channel 双向流的核心价值——插件能用内核服务，而不只是被动被调。
func TestPluginHostCall_PluginCallsBackIntoKernel(t *testing.T) {
	bin := buildPlugin(t, "./plugins/com.vastplan.hello-world/backend")
	host := newHost(t, "0.1.0")
	allowAllPermissions(t, host) // 本测试不测权限，显式放行
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	p, err := host.Launch(ctx, bin)
	if err != nil {
		t.Fatalf("装载插件失败: %v", err)
	}
	defer func() { _ = host.Close(p) }()

	// whoami 内部会回调宿主的 kernel.info
	resp, err := host.Invoke(ctx, toolTarget("vastplan.hello", "whoami"), testCallContext(), []byte(`{}`))
	if err != nil {
		t.Fatalf("调用 whoami 传输层失败: %v", err)
	}
	if resp.Result.Status != contractv1.CallResult_STATUS_OK {
		t.Fatalf("whoami 应成功，实际 %v", resp.Result.Error)
	}

	var got struct {
		Plugin       string         `json:"plugin"`
		HostReported map[string]any `json:"hostReported"`
	}
	if err := json.Unmarshal(resp.Payload, &got); err != nil {
		t.Fatalf("结果解析失败: %v", err)
	}
	if got.HostReported["kernel"] != kernelName || got.HostReported["version"] != "0.1.0" {
		t.Fatalf("插件未拿到正确的内核信息，实际: %v", got.HostReported)
	}
	// 插件回调是新的信任边界：租户和 trace 继续传播，但 Caller 必须由宿主按
	// 已认证 session 重建，不能让插件冒用原始 Agent 身份。
	if got.HostReported["tenant"] != "acme" || got.HostReported["traceId"] != "trace-e2e" ||
		got.HostReported["callerKind"] != "CALLER_KIND_PLUGIN" ||
		got.HostReported["callerId"] != "com.vastplan.hello-world" {
		t.Fatalf("HostCall 信任边界裁剪错误，实际: %v", got.HostReported)
	}
}

// 应用层错误须经 CallResult 返回，且与传输层错误严格区分（工程规范 §4.2）。
func TestPluginInvoke_ApplicationErrorsAreNotTransportErrors(t *testing.T) {
	bin := buildPlugin(t, "./plugins/com.vastplan.hello-world/backend")
	host := newHost(t, "0.1.0")
	allowAllPermissions(t, host) // 本测试不测权限，显式放行
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	p, err := host.Launch(ctx, bin)
	if err != nil {
		t.Fatalf("装载插件失败: %v", err)
	}
	defer func() { _ = host.Close(p) }()

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
			resp, err := host.Invoke(ctx, toolTarget("vastplan.hello", c.op), testCallContext(), []byte(c.payload))
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
	allowAllPermissions(t, host) // 本测试不测权限，显式放行
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	p, err := host.Launch(ctx, bin)
	if err != nil {
		t.Fatalf("装载插件失败: %v", err)
	}
	defer func() { _ = host.Close(p) }()

	if _, err := host.Invoke(ctx, &contractv1.CallTarget{
		ExtensionPoint: "tool.package", Capability: "not.registered",
	}, testCallContext(), nil); err == nil {
		t.Fatal("未注册能力应被拒绝，实际通过了")
	}
}

// engines fail-closed：内核版本不满足插件声明范围时必须拒绝装载
// （ADR-0017 §4 强制点 2）。这是版本机制的核心保障，必须由真实链路验证。
func TestPluginLaunch_IncompatibleKernelVersionRejected(t *testing.T) {
	bin := buildPlugin(t, "./plugins/com.vastplan.hello-world/backend")
	host := newHost(t, "0.2.0") // 插件要求 ^0.1，0.2.0 不满足

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	p, err := host.Launch(ctx, bin)
	if err == nil {
		_ = host.Close(p)
		t.Fatal("内核版本不兼容时应拒绝装载，实际装上了")
	}
	if !strings.Contains(err.Error(), "不满足插件要求") {
		t.Fatalf("错误信息应说明版本不满足，实际: %v", err)
	}
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
		_ = host.Close(p)
		t.Fatal("未声明本内核 engines 的插件应被拒绝，实际装上了")
	}
	if !strings.Contains(err.Error(), "未声明") {
		t.Fatalf("错误信息应说明未声明 engines，实际: %v", err)
	}
}

// descriptor 是协议消息的一部分，不能只相信制品发布时的清单校验。真实插件进程
// 若上报了不符合对应扩展点 Schema 的 descriptor，宿主应拒绝该贡献而不是污染 Registry。
// 协议允许部分接受，所以插件仍可完成激活；关键断言是非法能力绝不进入分发表。
func TestPluginRegistration_InvalidDescriptorRejected(t *testing.T) {
	bin := buildPlugin(t, "./e2e/fixtures/plugins/invalid-descriptor")
	host := newHost(t, "0.1.0")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	p, err := host.Launch(ctx, bin)
	if err != nil {
		t.Fatalf("包含被拒贡献的插件仍应按协议完成激活: %v", err)
	}
	defer func() { _ = host.Close(p) }()

	if got := host.Registry.List("hook"); len(got) != 0 {
		t.Fatalf("非法 hook descriptor 不得进入 Registry，实际: %+v", got)
	}
}

// 插件崩溃（SIGKILL，不走优雅退出）时：宿主须感知断连、摘除其贡献，
// 且**在途调用立刻脱身**而非挂到超时——这是 ADR-0004 故障隔离的实质。
func TestPluginCrash_ContributionsRemovedAndInflightCallsFail(t *testing.T) {
	bin := buildPlugin(t, "./e2e/fixtures/plugins/crasher")
	host := newHost(t, "0.1.0")
	allowAllPermissions(t, host) // 本测试不测权限，显式放行
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	p, err := host.Launch(ctx, bin)
	if err != nil {
		t.Fatalf("装载夹具插件失败: %v", err)
	}
	_ = p

	// 先确认它是活的
	if _, err := host.Invoke(ctx, toolTarget("fixture.crasher", "ping"), testCallContext(), []byte(`{}`)); err != nil {
		t.Fatalf("崩溃前 ping 应成功，实际: %v", err)
	}
	if _, ok := host.Registry.Lookup("tool.package", "fixture.crasher"); !ok {
		t.Fatal("贡献应已注册")
	}

	// 触发崩溃：该调用永不会有响应，插件会自杀
	start := time.Now()
	_, err = host.Invoke(ctx, toolTarget("fixture.crasher", "crash"), testCallContext(), []byte(`{}`))
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("插件崩溃后在途调用应失败，实际成功了")
	}
	// 关键：靠"感知断连"脱身，而不是靠调用超时（CallTimeout=5s）
	if elapsed >= 5*time.Second {
		t.Fatalf("在途调用应在插件崩溃时立刻失败，实际等了 %v（疑似挂到了超时）", elapsed)
	}
	if !strings.Contains(err.Error(), "失联") {
		t.Fatalf("错误应说明插件失联，实际: %v", err)
	}

	// 崩溃后贡献必须被摘除，否则宿主会把调用继续路由给一个死掉的插件
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := host.Registry.Lookup("tool.package", "fixture.crasher"); !ok {
			return // 已摘除
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("插件崩溃后其贡献应被摘除，实际仍在注册表中")
}

// DRAIN 必须先拒绝新调用，再等待已经进入插件处理器的调用完成；只有枚举和 Ack
// 而不等待在途工作，会让自动升级在切换时截断用户请求。
func TestPluginDrain_WaitsForInflightAndRejectsNewCalls(t *testing.T) {
	bin := buildPlugin(t, "./e2e/fixtures/plugins/crasher")
	host := newHost(t, "0.1.0")
	allowAllPermissions(t, host)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	p, err := host.Launch(ctx, bin)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = host.Close(p) }()

	callDone := make(chan error, 1)
	go func() {
		resp, err := host.Invoke(ctx, toolTarget("fixture.crasher", "slow"), testCallContext(), nil)
		if err == nil && resp.Result.Status != contractv1.CallResult_STATUS_OK {
			err = fmt.Errorf("slow 返回非 OK: %v", resp.Result)
		}
		callDone <- err
	}()
	time.Sleep(100 * time.Millisecond) // slow 已进入 400ms 处理器，留下约 300ms 在途窗口。
	started := time.Now()
	if err := host.Drain(ctx); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed < 200*time.Millisecond {
		t.Fatalf("DRAIN 未等待在途调用，耗时仅 %v", elapsed)
	}
	if err := <-callDone; err != nil {
		t.Fatalf("在途调用应正常完成: %v", err)
	}
	resp, err := host.Invoke(ctx, toolTarget("fixture.crasher", "ping"), testCallContext(), nil)
	if err != nil {
		t.Fatalf("drain 后拒绝应是应用层结果，不是传输错误: %v", err)
	}
	if resp.Result.Status != contractv1.CallResult_STATUS_ERROR || resp.Result.Error.Code != "plugin.inactive" {
		t.Fatalf("drain 后新调用应被 plugin.inactive 拒绝: %+v", resp.Result)
	}
}
