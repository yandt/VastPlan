//go:build e2e

// 分发语义的跨进程真实链路 E2E：select（permission.checker）与 fanout（event.sink）。
// 规格见《插件契约与协议》第四章 §4.2/§4.3、ADR-0021。
package e2e

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	contractv1 "cdsoft.com.cn/VastPlan/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/shared/go/extpoint"
	"cdsoft.com.cn/VastPlan/shared/go/protocolbus"
)

// launchAll 装载多个插件（顺序即启动顺序）。
func launchAll(t *testing.T, host *protocolbus.Host, pkgDirs ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	for _, dir := range pkgDirs {
		if _, err := host.Launch(ctx, buildPlugin(t, dir)); err != nil {
			t.Fatalf("装载 %s 失败: %v", dir, err)
		}
	}
}

// 零权限校验器时必须 fail-closed 拒绝——没装权限插件 = 无授权依据（ADR-0021）。
// 这条最要紧：它防止"忘装权限插件"变成静默的全开放。
func TestSelect_NoCheckerRegisteredDeniesEverything(t *testing.T) {
	host := newHost(t, "0.1.0")
	launchAll(t, host, "./plugins/com.vastplan.hello-world/backend") // 故意不装权限插件

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := host.Invoke(ctx, toolTarget("vastplan.hello", "greet"), testCallContext(), []byte(`{"name":"X"}`))
	if err != nil {
		t.Fatalf("权限拒绝应是应用层错误，不应冒泡为传输层错误: %v", err)
	}
	if resp.Result.Status != contractv1.CallResult_STATUS_ERROR {
		t.Fatal("无权限校验器时应拒绝，实际放行了——这会让忘装权限插件变成静默全开放")
	}
	if resp.Result.Error.Code != "permission.denied" {
		t.Fatalf("错误码应为 permission.denied，实际 %q", resp.Result.Error.Code)
	}
}

// 装上权限插件后，可信调用方（agent）应被基线策略放行。
func TestSelect_BaselineAllowsTrustedCaller(t *testing.T) {
	host := newHost(t, "0.1.0")
	launchAll(t, host,
		"./plugins/com.vastplan.demo-permission/backend",
		"./plugins/com.vastplan.hello-world/backend")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := host.Invoke(ctx, toolTarget("vastplan.hello", "greet"), testCallContext(), []byte(`{"name":"X"}`))
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if resp.Result.Status != contractv1.CallResult_STATUS_OK {
		t.Fatalf("agent 调用方应被基线策略放行，实际: %v", resp.Result.Error)
	}
}

// 高优先级校验器一旦定论（deny），低优先级的放行结论不得翻盘——
// 这是 select「按 priority 高→低，第一个非 abstain 即定论」的实质。
func TestSelect_HigherPriorityDenyWinsOverLowerPriorityAllow(t *testing.T) {
	host := newHost(t, "0.1.0")
	launchAll(t, host, "./plugins/com.vastplan.demo-permission/backend")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// vastplan.forbidden 在 demo.denylist(priority 100) 的拒绝名单中；
	// 而 demo.baseline(priority 10) 对 admin 一律放行——高优先级必须赢。
	adminCtx := testCallContext()
	adminCtx.Principal.IsAdmin = true

	res := host.CheckPermission(ctx, adminCtx, &contractv1.CallTarget{
		ExtensionPoint: "tool.package", Capability: "vastplan.forbidden",
	})
	if res.Allowed() {
		t.Fatal("高优先级拒绝名单应胜过低优先级的管理员放行，实际放行了")
	}
	if res.DecidedBy != "demo.denylist" {
		t.Fatalf("应由 demo.denylist 定论，实际由 %q", res.DecidedBy)
	}
}

// 高优先级弃权时应继续问下一个——abstain 不是结论。
func TestSelect_AbstainFallsThroughToNextChecker(t *testing.T) {
	host := newHost(t, "0.1.0")
	launchAll(t, host, "./plugins/com.vastplan.demo-permission/backend")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	adminCtx := testCallContext()
	adminCtx.Principal.IsAdmin = true

	// 不在拒绝名单 → denylist 弃权 → 应由 baseline 定论
	res := host.CheckPermission(ctx, adminCtx, toolTarget("vastplan.hello", "greet"))
	if !res.Allowed() {
		t.Fatalf("denylist 弃权后应由 baseline 放行，实际: %+v", res)
	}
	if res.DecidedBy != "demo.baseline" {
		t.Fatalf("应由 demo.baseline 定论（denylist 已弃权），实际由 %q", res.DecidedBy)
	}
}

// 全部校验器弃权 → fail-closed 拒绝。
func TestSelect_AllAbstainIsFailClosed(t *testing.T) {
	host := newHost(t, "0.1.0")
	launchAll(t, host, "./plugins/com.vastplan.demo-permission/backend")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 非管理员的 user 调用方：denylist 弃权、baseline 也弃权 → 应拒绝
	userCtx := testCallContext()
	userCtx.Caller = &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_USER, Id: "u-1"}
	userCtx.Principal.IsAdmin = false

	res := host.CheckPermission(ctx, userCtx, toolTarget("vastplan.hello", "greet"))
	if res.Allowed() {
		t.Fatal("全部弃权时应 fail-closed 拒绝，实际放行了")
	}
	if res.DecidedBy != "" {
		t.Fatalf("无人定论时 DecidedBy 应为空，实际 %q", res.DecidedBy)
	}
}

// fanout：事件应扇出给**订阅了该类型**的 sink，且真的送达插件（查账本验证）。
func TestFanout_EventDeliveredToSubscribedSink(t *testing.T) {
	host := newHost(t, "0.1.0")
	launchAll(t, host,
		"./plugins/com.vastplan.demo-permission/backend",
		"./plugins/com.vastplan.demo-audit/backend")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	outcomes := host.PublishEvent(ctx, &contractv1.CallEvent{
		Id: "evt-1", Type: "task.completed", Source: "kernel-test",
		TenantId: "acme", OccurredAtUnixMs: time.Now().UnixMilli(),
	})
	if len(outcomes) != 1 {
		t.Fatalf("应扇出给 1 个订阅者，实际 %d 个", len(outcomes))
	}
	if outcomes[0].Err != nil {
		t.Fatalf("事件汇处理失败: %v", outcomes[0].Err)
	}

	// 关键：向审计插件查账，确认事件**真的送达了插件**而非宿主自说自话
	resp, err := host.Invoke(ctx, toolTarget("demo.audit", "list"), testCallContext(), nil)
	if err != nil {
		t.Fatalf("查审计账本失败: %v", err)
	}
	var book struct {
		Events []struct {
			Type   string `json:"type"`
			Source string `json:"source"`
		} `json:"events"`
	}
	if err := json.Unmarshal(resp.Payload, &book); err != nil {
		t.Fatalf("账本解析失败: %v", err)
	}
	if len(book.Events) != 1 || book.Events[0].Type != "task.completed" ||
		book.Events[0].Source != "kernel-test" {
		t.Fatalf("审计插件未收到预期事件，账本: %+v", book.Events)
	}
}

// fanout 按订阅过滤：未被订阅的事件类型不得投递（fail-closed：没声明就别收）。
func TestFanout_UnsubscribedEventTypeNotDelivered(t *testing.T) {
	host := newHost(t, "0.1.0")
	launchAll(t, host,
		"./plugins/com.vastplan.demo-permission/backend",
		"./plugins/com.vastplan.demo-audit/backend")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// demo.audit.sink 只订阅 task.* 与 plugin.activated
	outcomes := host.PublishEvent(ctx, &contractv1.CallEvent{
		Id: "evt-x", Type: "unsubscribed.type", Source: "kernel-test", TenantId: "acme",
	})
	if len(outcomes) != 0 {
		t.Fatalf("未订阅的类型不应投递，实际投给了 %d 个 sink", len(outcomes))
	}
}

// 订阅 glob 匹配：task.* 应匹配 task.started / task.completed，但不匹配 other.thing。
func TestFanout_SubscribeGlobMatching(t *testing.T) {
	cases := []struct {
		eventType string
		want      bool
	}{
		{"task.completed", true},
		{"task.started", true},
		{"plugin.activated", true}, // 精确订阅
		{"other.thing", false},
		{"task", false}, // 无点号后缀，不匹配 task.*
	}
	d := &extpoint.SinkDescriptor{Subscribe: []string{"task.*", "plugin.activated"}}
	for _, c := range cases {
		if got := d.Subscribes(c.eventType); got != c.want {
			t.Errorf("Subscribes(%q) = %v，期望 %v", c.eventType, got, c.want)
		}
	}
}

// Hook 是有序中间件而非事件汇：before 可否决，after 只观察已完成的结论。
// 该用例走真实进程、真实贡献注册与真实 Channel，确认示例配额插件不是仅有声明。
func TestHook_BeforeAbortsAndAfterMeters(t *testing.T) {
	host := newHost(t, "0.1.0")
	launchAll(t, host,
		"./plugins/com.vastplan.demo-permission/backend",
		"./plugins/com.vastplan.demo-quota/backend",
		"./plugins/com.vastplan.hello-world/backend")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// demo.quota.limit 的上限为 3：前三次应放行，且 each completion 应被 after 钩子计量。
	for i := 1; i <= 3; i++ {
		resp, err := host.Invoke(ctx, toolTarget("vastplan.hello", "greet"), testCallContext(), []byte(`{"name":"Hook"}`))
		if err != nil {
			t.Fatalf("第 %d 次调用出现传输层错误: %v", i, err)
		}
		if resp.Result.Status != contractv1.CallResult_STATUS_OK {
			t.Fatalf("第 %d 次调用应被配额放行，实际: %+v", i, resp.Result)
		}
	}

	// 第四次由 before 钩子直接否决；它不应继续进入目标工具或 after 计量。
	resp, err := host.Invoke(ctx, toolTarget("vastplan.hello", "greet"), testCallContext(), []byte(`{"name":"Hook"}`))
	if err != nil {
		t.Fatalf("配额否决应是应用层结果，不应冒泡为传输层错误: %v", err)
	}
	if resp.Result.Status != contractv1.CallResult_STATUS_ERROR || resp.Result.Error.GetCode() != "hook.aborted" {
		t.Fatalf("第四次调用应返回 hook.aborted，实际: %+v", resp.Result)
	}

	// 通过插件公开工具查账，证明 after 钩子收到的是前三次真实调用，而不是宿主侧的虚假记录。
	resp, err = host.Invoke(ctx, toolTarget("demo.quota", "usage"), testCallContext(), nil)
	if err != nil {
		t.Fatalf("查询配额计量失败: %v", err)
	}
	var usage struct {
		Metered []struct {
			Capability string `json:"capability"`
			Status     string `json:"status"`
		} `json:"metered"`
	}
	if err := json.Unmarshal(resp.Payload, &usage); err != nil {
		t.Fatalf("配额计量结果解析失败: %v", err)
	}
	if len(usage.Metered) != 3 {
		t.Fatalf("after 钩子应只计量 3 次已放行调用，实际: %+v", usage.Metered)
	}
	for i, record := range usage.Metered {
		if record.Capability != "vastplan.hello" || record.Status != "STATUS_OK" {
			t.Fatalf("第 %d 条计量记录不符合预期: %+v", i, record)
		}
	}
}
