package platformcontrol

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"
	"time"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
	platformcontrolv1 "cdsoft.com.cn/VastPlan/contracts/schemas/platformcontrol/v1"
	sharedstatesqlv1 "cdsoft.com.cn/VastPlan/contracts/schemas/sharedstatesql/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/sharedstate"
)

type testInvoker struct {
	calls     []string
	instances map[string][]RuntimeInstance
	result    func(string, string, string) (*contractv1.CallResult, []byte, error)
}

func (i *testInvoker) Invoke(_ context.Context, capability, operation string, _ []byte) (*contractv1.CallResult, []byte, error) {
	return i.respond(capability, operation, "")
}

func (i *testInvoker) InvokeInstance(_ context.Context, capability, operation, instanceID string, _ []byte) (*contractv1.CallResult, []byte, error) {
	return i.respond(capability, operation, instanceID)
}

func (i *testInvoker) Instances(capability string) []RuntimeInstance {
	return append([]RuntimeInstance(nil), i.instances[capability]...)
}

func (i *testInvoker) respond(capability, operation, instanceID string) (*contractv1.CallResult, []byte, error) {
	i.calls = append(i.calls, capability+"/"+operation+"@"+instanceID)
	if i.result != nil {
		return i.result(capability, operation, instanceID)
	}
	if capability == platformcontrolv1.BootstrapCapability {
		raw, _ := json.Marshal(platformcontrolv1.Status{Phase: platformcontrolv1.PhaseReady, Generation: 1})
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
	}
	if operation == sharedstatesqlv1.OperationList {
		raw, _ := json.Marshal(sharedstatesqlv1.Page{Items: []sharedstatesqlv1.Entry{{Key: "active", ValueBase64: base64.StdEncoding.EncodeToString([]byte("value")), Revision: 2, UpdatedAt: time.Unix(1, 0)}}})
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
	}
	raw, _ := json.Marshal(sharedstatesqlv1.Entry{Key: "active", ValueBase64: base64.StdEncoding.EncodeToString([]byte("value")), Revision: 2, UpdatedAt: time.Unix(1, 0)})
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
}

func TestRemoteBootstrapperAndStoreUseSeparateCapabilities(t *testing.T) {
	instances := []RuntimeInstance{{ID: "runtime-local", NodeID: "node-local"}, {ID: "runtime-remote", NodeID: "node-remote"}}
	invoke := &testInvoker{instances: map[string][]RuntimeInstance{
		platformcontrolv1.BootstrapCapability: instances,
		sharedstatesqlv1.Capability:           instances,
	}}
	bootstrapper, _ := NewRemoteBootstrapper(invoke)
	profile := platformcontrolv1.Profile{SchemaVersion: 1, Generation: 1}
	store, err := bootstrapper.Initialize(context.Background(), profile, testSecret("secret"))
	if err != nil {
		t.Fatal(err)
	}
	scope := sharedstate.Scope{Kind: sharedstate.ScopeService, PluginID: "cn.vastplan.test", RuntimeScope: "service", Namespace: "state"}
	page, err := store.List(context.Background(), scope, "", 20, "")
	if err != nil || len(page.Items) != 1 || string(page.Items[0].Value) != "value" {
		t.Fatalf("远端 Store 转换失败: page=%+v err=%v", page, err)
	}
	if len(invoke.calls) != 3 ||
		invoke.calls[0] != platformcontrolv1.BootstrapCapability+"/"+platformcontrolv1.OperationInitialize+"@runtime-local" ||
		invoke.calls[1] != platformcontrolv1.BootstrapCapability+"/"+platformcontrolv1.OperationOpen+"@runtime-remote" ||
		invoke.calls[2] != sharedstatesqlv1.Capability+"/"+sharedstatesqlv1.OperationList+"@runtime-local" {
		t.Fatalf("Capability 路由错误: %v", invoke.calls)
	}
}

func TestRemoteBootstrapperPreservesSafeDatabaseFailure(t *testing.T) {
	invoke := &testInvoker{instances: map[string][]RuntimeInstance{
		platformcontrolv1.BootstrapCapability: {{ID: "runtime-a"}},
	}, result: func(_, _, _ string) (*contractv1.CallResult, []byte, error) {
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{
			Code: databasev1.ErrorAuthenticationFailed, Message: "safe message", Retryable: false,
		}}, nil, nil
	}}
	bootstrapper, _ := NewRemoteBootstrapper(invoke)
	err := bootstrapper.Test(context.Background(), platformcontrolv1.Profile{SchemaVersion: 1, Generation: 1}, testSecret("secret"))
	code, retryable, ok := FailureDetails(err)
	if !ok || code != databasev1.ErrorAuthenticationFailed || retryable {
		t.Fatalf("跨进程数据库诊断丢失: code=%s retryable=%v ok=%v err=%v", code, retryable, ok, err)
	}
}

func TestRemoteBootstrapperProvisionsOnlyOneTrustedRuntimeInstance(t *testing.T) {
	invoke := &testInvoker{instances: map[string][]RuntimeInstance{
		platformcontrolv1.BootstrapCapability: {{ID: "runtime-a"}, {ID: "runtime-b"}},
	}}
	bootstrapper, _ := NewRemoteBootstrapper(invoke)
	if err := bootstrapper.Provision(context.Background(), platformcontrolv1.Profile{SchemaVersion: 1, Generation: 1}, testSecret("secret")); err != nil {
		t.Fatal(err)
	}
	want := platformcontrolv1.BootstrapCapability + "/" + platformcontrolv1.OperationProvision + "@runtime-a"
	if len(invoke.calls) != 1 || invoke.calls[0] != want {
		t.Fatalf("建库必须只由首个可信 Runtime 执行: %v", invoke.calls)
	}
}

func TestRemoteStorePreservesStableConflict(t *testing.T) {
	invoke := &testInvoker{instances: map[string][]RuntimeInstance{sharedstatesqlv1.Capability: {{ID: "runtime-a"}}}, result: func(_, _, _ string) (*contractv1.CallResult, []byte, error) {
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: sharedstatesqlv1.ErrorConflict}}, nil, nil
	}}
	replicas := newRuntimeReplicaSet()
	replicas.Replace([]string{"runtime-a"})
	store := &RemoteStore{invoke: invoke, replicas: replicas}
	scope := sharedstate.Scope{Kind: sharedstate.ScopeService, PluginID: "cn.vastplan.test", RuntimeScope: "service", Namespace: "state"}
	if _, err := store.Update(context.Background(), scope, "active", []byte("v"), 1); err != sharedstate.ErrConflict {
		t.Fatalf("稳定冲突语义丢失: %v", err)
	}
}

func TestRemoteStoreFailsOverTransportButNeverReplaysApplicationFailure(t *testing.T) {
	instances := []RuntimeInstance{{ID: "runtime-local"}, {ID: "runtime-remote-a"}, {ID: "runtime-remote-b"}}
	invoke := &testInvoker{instances: map[string][]RuntimeInstance{sharedstatesqlv1.Capability: instances}, result: func(_, _, instanceID string) (*contractv1.CallResult, []byte, error) {
		if instanceID != "runtime-remote-b" {
			return nil, nil, errors.New("runtime transport unavailable")
		}
		raw, _ := json.Marshal(sharedstatesqlv1.Entry{Key: "active", ValueBase64: base64.StdEncoding.EncodeToString([]byte("remote")), Revision: 3, UpdatedAt: time.Unix(1, 0)})
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
	}}
	replicas := newRuntimeReplicaSet()
	replicas.Replace([]string{"runtime-local", "runtime-remote-a", "runtime-remote-b"})
	store := &RemoteStore{invoke: invoke, replicas: replicas}
	scope := sharedstate.Scope{Kind: sharedstate.ScopeService, PluginID: "cn.vastplan.test", RuntimeScope: "service", Namespace: "state"}
	entry, err := store.Get(context.Background(), scope, "active")
	if err != nil || string(entry.Value) != "remote" || len(invoke.calls) != 3 {
		t.Fatalf("传输故障未安全转移: entry=%+v calls=%v err=%v", entry, invoke.calls, err)
	}

	invoke.calls = nil
	invoke.result = func(_, _, _ string) (*contractv1.CallResult, []byte, error) {
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: sharedstatesqlv1.ErrorConflict}}, nil, nil
	}
	if _, err := store.Get(context.Background(), scope, "active"); !errors.Is(err, sharedstate.ErrConflict) || len(invoke.calls) != 1 {
		t.Fatalf("应用失败不得跨副本重放: calls=%v err=%v", invoke.calls, err)
	}
}

func TestTopologyOpenFailureKeepsLastKnownGoodReplicaSet(t *testing.T) {
	invoke := &testInvoker{instances: map[string][]RuntimeInstance{
		platformcontrolv1.BootstrapCapability: {{ID: "runtime-a"}},
		sharedstatesqlv1.Capability:           {{ID: "runtime-a"}},
	}}
	bootstrapper, _ := NewRemoteBootstrapper(invoke)
	profile := platformcontrolv1.Profile{SchemaVersion: 1, Generation: 1}
	store, err := bootstrapper.Open(context.Background(), profile, testSecret("secret"))
	if err != nil {
		t.Fatal(err)
	}

	invoke.instances[platformcontrolv1.BootstrapCapability] = []RuntimeInstance{{ID: "runtime-a"}, {ID: "runtime-b"}}
	invoke.instances[sharedstatesqlv1.Capability] = []RuntimeInstance{{ID: "runtime-a"}, {ID: "runtime-b"}}
	invoke.result = func(capability, operation, instanceID string) (*contractv1.CallResult, []byte, error) {
		if capability == platformcontrolv1.BootstrapCapability && operation == platformcontrolv1.OperationOpen && instanceID == "runtime-b" {
			return nil, nil, errors.New("new replica unavailable")
		}
		return (&testInvoker{}).respond(capability, operation, instanceID)
	}
	if _, err := bootstrapper.Open(context.Background(), profile, testSecret("secret")); err == nil {
		t.Fatal("新副本打开失败必须让本次拓扑更新失败")
	}

	invoke.result = nil
	invoke.instances[sharedstatesqlv1.Capability] = []RuntimeInstance{{ID: "runtime-a"}, {ID: "runtime-b"}}
	scope := sharedstate.Scope{Kind: sharedstate.ScopeService, PluginID: "cn.vastplan.test", RuntimeScope: "service", Namespace: "state"}
	if _, err := store.Get(context.Background(), scope, "active"); err != nil {
		t.Fatal(err)
	}
	if got := invoke.calls[len(invoke.calls)-1]; got != sharedstatesqlv1.Capability+"/"+sharedstatesqlv1.OperationGet+"@runtime-a" {
		t.Fatalf("失败拓扑不得把未打开副本加入调用集合: %s", got)
	}
}

func TestTopologyReopenUpdatesExistingRemoteStoreReplicaSet(t *testing.T) {
	invoke := &testInvoker{instances: map[string][]RuntimeInstance{
		platformcontrolv1.BootstrapCapability: {{ID: "runtime-a"}},
		sharedstatesqlv1.Capability:           {{ID: "runtime-a"}},
	}}
	bootstrapper, _ := NewRemoteBootstrapper(invoke)
	profile := platformcontrolv1.Profile{SchemaVersion: 1, Generation: 1}
	store, err := bootstrapper.Open(context.Background(), profile, testSecret("secret"))
	if err != nil {
		t.Fatal(err)
	}
	invoke.instances[platformcontrolv1.BootstrapCapability] = []RuntimeInstance{{ID: "runtime-a"}, {ID: "runtime-b"}}
	invoke.instances[sharedstatesqlv1.Capability] = []RuntimeInstance{{ID: "runtime-a"}, {ID: "runtime-b"}}
	if _, err := bootstrapper.Open(context.Background(), profile, testSecret("secret")); err != nil {
		t.Fatal(err)
	}
	// Simulate runtime-a leaving after b was opened. The store returned by the
	// first bind must observe the shared replica set without kernel rebinding.
	invoke.instances[sharedstatesqlv1.Capability] = []RuntimeInstance{{ID: "runtime-b"}}
	scope := sharedstate.Scope{Kind: sharedstate.ScopeService, PluginID: "cn.vastplan.test", RuntimeScope: "service", Namespace: "state"}
	if _, err := store.Get(context.Background(), scope, "active"); err != nil || invoke.calls[len(invoke.calls)-1] != sharedstatesqlv1.Capability+"/"+sharedstatesqlv1.OperationGet+"@runtime-b" {
		t.Fatalf("既有 Store 未切换到新打开副本: calls=%v err=%v", invoke.calls, err)
	}
}

func TestAwaitCandidateOpenWaitsForAllSuccessReplicaReplacement(t *testing.T) {
	invoke := &testInvoker{instances: map[string][]RuntimeInstance{
		platformcontrolv1.BootstrapCapability: {{ID: "runtime-old"}, {ID: "runtime-candidate"}},
	}}
	bootstrapper, _ := NewRemoteBootstrapper(invoke)
	bootstrapper.replicas.Replace([]string{"runtime-old"})

	type result struct {
		relevant bool
		err      error
	}
	done := make(chan result, 1)
	go func() {
		relevant, err := bootstrapper.AwaitCandidateOpen(context.Background(), []string{"runtime-candidate"})
		done <- result{relevant: relevant, err: err}
	}()
	select {
	case got := <-done:
		t.Fatalf("候选进入 all-success Open 前不得越过屏障: %+v", got)
	case <-time.After(20 * time.Millisecond):
	}
	bootstrapper.replicas.Replace([]string{"runtime-old", "runtime-candidate"})
	select {
	case got := <-done:
		if !got.relevant || got.err != nil {
			t.Fatalf("候选 Open 后屏障结果错误: %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("候选 Open 后屏障未释放")
	}
}

func TestAwaitCandidateOpenIgnoresUnitWithoutBootstrapCapability(t *testing.T) {
	invoke := &testInvoker{instances: map[string][]RuntimeInstance{
		platformcontrolv1.BootstrapCapability: {{ID: "database-runtime"}},
	}}
	bootstrapper, _ := NewRemoteBootstrapper(invoke)
	relevant, err := bootstrapper.AwaitCandidateOpen(context.Background(), []string{"unrelated-bootstrap-unit"})
	if err != nil || relevant {
		t.Fatalf("不贡献 Bootstrap capability 的单元不应进入 Platform Control 屏障: relevant=%v err=%v", relevant, err)
	}
}
