package protocolbus

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/errorcode"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	"cdsoft.com.cn/VastPlan/core/shared/go/registry"
	"cdsoft.com.cn/VastPlan/core/shared/go/runtimeidentity"
)

const (
	embeddedTestPlugin   = "cn.vastplan.test.embedded"
	embeddedTestTool     = "test.embedded.tool"
	embeddedTestPolicyID = "test.embedded.allow"
)

var (
	embeddedToolDescriptor   = []byte(`{"title":"内嵌测试","subcommands":[{"name":"run","description":"运行"}]}`)
	embeddedPolicyDescriptor = []byte(`{"title":"测试放行策略","applies":{}}`)
)

func newEmbeddedTestHost(t testing.TB) *Host {
	t.Helper()
	reg := registry.New()
	reg.DefinePoint(registry.ExtensionPoint{Name: extpoint.ToolPackage, Dispatch: registry.DispatchSingle})
	reg.DefinePoint(registry.ExtensionPoint{Name: extpoint.PermissionChecker, Dispatch: registry.DispatchSelect})
	reg.DefinePoint(registry.ExtensionPoint{Name: extpoint.KernelService, Dispatch: registry.DispatchSingle})
	return NewHost("backend", "1.0.0", reg, nil)
}

func embeddedTestDefinition(tool EmbeddedHandler) EmbeddedPlugin {
	allow := func(context.Context, EmbeddedHost, *contractv1.CallContext, []byte) (*contractv1.CallResult, []byte, error) {
		payload, _ := json.Marshal(extpoint.PermissionResponse{Decision: extpoint.DecisionAllow, Reason: "test"})
		return embeddedOK(), payload, nil
	}
	return EmbeddedPlugin{ID: embeddedTestPlugin, Version: "1.0.0", Contributions: []EmbeddedContribution{
		{ExtensionPoint: extpoint.PermissionChecker, ID: embeddedTestPolicyID, Priority: 100,
			Descriptor: embeddedPolicyDescriptor, Handlers: map[string]EmbeddedHandler{"check": allow}},
		{ExtensionPoint: extpoint.ToolPackage, ID: embeddedTestTool,
			Descriptor: embeddedToolDescriptor, Handlers: map[string]EmbeddedHandler{"run": tool}},
	}}
}

func embeddedTestPolicy() LaunchPolicy {
	return LaunchPolicy{PluginID: embeddedTestPlugin, Version: "1.0.0", Contributions: []pluginv1.RuntimeContribution{
		{ExtensionPoint: extpoint.PermissionChecker, ID: embeddedTestPolicyID, Priority: 100, Descriptor: embeddedPolicyDescriptor},
		{ExtensionPoint: extpoint.ToolPackage, ID: embeddedTestTool, Descriptor: embeddedToolDescriptor},
	}}
}

func embeddedOK() *contractv1.CallResult {
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK, Usage: &contractv1.Usage{}}
}

func TestEmbeddedPluginUsesPublicSecurityPipeline(t *testing.T) {
	host := newEmbeddedTestHost(t)
	called := false
	definition := embeddedTestDefinition(func(context.Context, EmbeddedHost, *contractv1.CallContext, []byte) (*contractv1.CallResult, []byte, error) {
		called = true
		return embeddedOK(), []byte("ok"), nil
	})
	process, err := host.LaunchEmbeddedWithPolicy(context.Background(), definition, embeddedTestPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if process.RuntimeKind() != "embedded" || process.PID != 0 || !process.Alive() {
		t.Fatalf("内嵌实例状态错误: %+v", process)
	}
	op := "run"
	response, err := host.Invoke(context.Background(), &contractv1.CallTarget{
		ExtensionPoint: extpoint.ToolPackage, Capability: embeddedTestTool, Operation: &op,
	}, &contractv1.CallContext{Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_USER, Id: "user"}}, nil)
	if err != nil || response.Result.Status != contractv1.CallResult_STATUS_OK || string(response.Payload) != "ok" || !called {
		t.Fatalf("内嵌调用未通过统一管道: response=%+v err=%v called=%v", response, err, called)
	}
}

func TestEmbeddedHostCallReissuesAuthenticatedPluginIdentity(t *testing.T) {
	host := newEmbeddedTestHost(t)
	var caller *contractv1.Caller
	var runtimeIdentityOK bool
	if err := host.RegisterHostService(extpoint.KernelService, "kernel.test.identity",
		func(ctx context.Context, callCtx *contractv1.CallContext, _ []byte) (*contractv1.CallResult, []byte, error) {
			caller = callCtx.GetCaller()
			identity, ok := runtimeidentity.FromContext(ctx)
			runtimeIdentityOK = ok && identity.PluginID == embeddedTestPlugin && identity.Publisher == "vastplan" && identity.InstanceID == "runtime-test"
			return embeddedOK(), nil, nil
		}); err != nil {
		t.Fatal(err)
	}
	definition := embeddedTestDefinition(func(ctx context.Context, callback EmbeddedHost, _ *contractv1.CallContext, _ []byte) (*contractv1.CallResult, []byte, error) {
		forged := &contractv1.CallContext{Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_SYSTEM, Id: "forged"}}
		result, _, err := callback.Call(ctx, &contractv1.CallTarget{
			ExtensionPoint: extpoint.KernelService, Capability: "kernel.test.identity",
		}, forged, nil)
		return result, nil, err
	})
	policy := embeddedTestPolicy()
	policy.KernelServices = []string{"kernel.test.identity"}
	policy.Publisher = "vastplan"
	policy.ArtifactSHA256 = strings.Repeat("a", 64)
	policy.NodeID = "node-a"
	policy.RuntimeScope = "test-unit"
	policy.RuntimeInstanceID = "runtime-test"
	if _, err := host.LaunchEmbeddedWithPolicy(context.Background(), definition, policy); err != nil {
		t.Fatal(err)
	}
	op := "run"
	response, err := host.Invoke(context.Background(), &contractv1.CallTarget{
		ExtensionPoint: extpoint.ToolPackage, Capability: embeddedTestTool, Operation: &op,
	}, &contractv1.CallContext{Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_USER, Id: "user"}}, nil)
	if err != nil || response.Result.Status != contractv1.CallResult_STATUS_OK {
		t.Fatalf("调用失败: response=%+v err=%v", response, err)
	}
	if caller == nil || caller.Kind != contractv1.CallerKind_CALLER_KIND_PLUGIN || caller.Id != embeddedTestPlugin {
		t.Fatalf("伪造身份未被覆盖: %+v", caller)
	}
	if !runtimeIdentityOK {
		t.Fatal("本地 Kernel service 未收到 Host 会话绑定的 runtime identity")
	}
}

func TestEmbeddedManifestMismatchAndPanicFailClosed(t *testing.T) {
	host := newEmbeddedTestHost(t)
	definition := embeddedTestDefinition(func(context.Context, EmbeddedHost, *contractv1.CallContext, []byte) (*contractv1.CallResult, []byte, error) {
		panic("boom")
	})
	mismatch := embeddedTestPolicy()
	mismatch.Contributions[1].Priority = 99
	if _, err := host.LaunchEmbeddedWithPolicy(context.Background(), definition, mismatch); err == nil {
		t.Fatal("静态代码贡献与验签清单不一致时必须拒绝")
	}
	process, err := host.LaunchEmbeddedWithPolicy(context.Background(), definition, embeddedTestPolicy())
	if err != nil {
		t.Fatal(err)
	}
	op := "run"
	response, err := host.Invoke(context.Background(), &contractv1.CallTarget{
		ExtensionPoint: extpoint.ToolPackage, Capability: embeddedTestTool, Operation: &op,
	}, &contractv1.CallContext{}, nil)
	if err != nil || response.Result.GetError().GetCode() != errorcode.PluginHandlerError {
		t.Fatalf("panic 必须转换为稳定错误: response=%+v err=%v", response, err)
	}
	if process.Alive() || process.Err() == nil || !strings.Contains(process.Err().Error(), "panic") {
		t.Fatalf("panic 后实例必须死亡并保留原因: alive=%v err=%v", process.Alive(), process.Err())
	}
	if _, ok := host.Registry.Lookup(extpoint.ToolPackage, embeddedTestTool); ok {
		t.Fatal("panic 后必须摘除全部贡献")
	}
}

func BenchmarkEmbeddedPluginDispatch(b *testing.B) {
	host := newEmbeddedTestHost(b)
	definition := embeddedTestDefinition(func(context.Context, EmbeddedHost, *contractv1.CallContext, []byte) (*contractv1.CallResult, []byte, error) {
		return embeddedOK(), nil, nil
	})
	if _, err := host.LaunchEmbeddedWithPolicy(context.Background(), definition, embeddedTestPolicy()); err != nil {
		b.Fatal(err)
	}
	op := "run"
	target := &contractv1.CallTarget{ExtensionPoint: extpoint.ToolPackage, Capability: embeddedTestTool, Operation: &op}
	callCtx := &contractv1.CallContext{}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := host.invoke(context.Background(), target, callCtx, nil); err != nil {
			b.Fatal(err)
		}
	}
}
