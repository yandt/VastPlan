package platformcontrol

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	platformcontrolv1 "cdsoft.com.cn/VastPlan/contracts/schemas/platformcontrol/v1"
	sharedstatesqlv1 "cdsoft.com.cn/VastPlan/contracts/schemas/sharedstatesql/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/sharedstate"
)

type testInvoker struct{ calls []string }

func (i *testInvoker) Invoke(_ context.Context, capability, operation string, _ []byte) (*contractv1.CallResult, []byte, error) {
	i.calls = append(i.calls, capability+"/"+operation)
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
	invoke := &testInvoker{}
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
	if invoke.calls[0] != platformcontrolv1.BootstrapCapability+"/"+platformcontrolv1.OperationInitialize || invoke.calls[1] != sharedstatesqlv1.Capability+"/"+sharedstatesqlv1.OperationList {
		t.Fatalf("Capability 路由错误: %v", invoke.calls)
	}
}

func TestRemoteStorePreservesStableConflict(t *testing.T) {
	invoke := InvokerFunc(func(context.Context, string, string, []byte) (*contractv1.CallResult, []byte, error) {
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: sharedstatesqlv1.ErrorConflict}}, nil, nil
	})
	store := &RemoteStore{invoke: invoke}
	scope := sharedstate.Scope{Kind: sharedstate.ScopeService, PluginID: "cn.vastplan.test", RuntimeScope: "service", Namespace: "state"}
	if _, err := store.Update(context.Background(), scope, "active", []byte("v"), 1); err != sharedstate.ErrConflict {
		t.Fatalf("稳定冲突语义丢失: %v", err)
	}
}

type InvokerFunc func(context.Context, string, string, []byte) (*contractv1.CallResult, []byte, error)

func (f InvokerFunc) Invoke(ctx context.Context, capability, operation string, payload []byte) (*contractv1.CallResult, []byte, error) {
	return f(ctx, capability, operation, payload)
}
