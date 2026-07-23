package settings

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	sharedstatev1 "cdsoft.com.cn/VastPlan/contracts/schemas/sharedstate/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/sharedstate"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func TestDescriptorMatchesSignedManifest(t *testing.T) {
	assertDescriptorMatchesManifest(t, Capability, Descriptor())
}

func assertDescriptorMatchesManifest(t *testing.T, capability string, descriptor []byte) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "vastplan.plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := pluginv1.ParseManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	contributions, err := pluginv1.BackendRuntimeContributions(manifest)
	if err != nil {
		t.Fatal(err)
	}
	for _, contribution := range contributions {
		if contribution.ID != capability {
			continue
		}
		var signed, runtime any
		if json.Unmarshal(contribution.Descriptor, &signed) != nil || json.Unmarshal(descriptor, &runtime) != nil || !reflect.DeepEqual(signed, runtime) {
			t.Fatalf("运行时 descriptor 与签名 Manifest 不一致\nsigned=%s\nruntime=%s", contribution.Descriptor, descriptor)
		}
		return
	}
	t.Fatalf("Manifest 未声明 capability %s", capability)
}

func testCallContext(tenant string) *contractv1.CallContext {
	return &contractv1.CallContext{TenantId: tenant}
}

func TestSettingsUsesSharedTenantStateAcrossInstances(t *testing.T) {
	host := newSharedStateHost(t)
	first, second := New(), New()
	ctx := testCallContext("tenant-a")
	result, _, err := first.Handler(context.Background(), host, ctx, []byte(`{"key":"feature.enabled","value":true,"ifVersion":0}`), "put")
	if err != nil || result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("首次写入失败: result=%+v err=%v", result, err)
	}
	result, raw, err := second.Handler(context.Background(), host, ctx, []byte(`{"key":"feature.enabled"}`), "get")
	if err != nil || result.GetStatus() != contractv1.CallResult_STATUS_OK || !strings.Contains(string(raw), `"version":1`) {
		t.Fatalf("第二实例未读取共享状态: result=%+v raw=%s err=%v", result, raw, err)
	}
	result, _, _ = second.Handler(context.Background(), host, testCallContext("tenant-b"), []byte(`{"key":"feature.enabled"}`), "get")
	if result.GetError().GetCode() != "platform.settings.not_found" {
		t.Fatalf("跨 tenant 状态泄漏: %+v", result)
	}
	for _, call := range host.callsSnapshot() {
		if strings.Contains(string(call.payload), "tenant-a") || strings.Contains(string(call.payload), PluginID) || strings.Contains(string(call.payload), "runtimeScope") {
			t.Fatalf("Shared State 请求泄漏可信身份: %s", call.payload)
		}
	}
}

func TestSettingsConcurrentCASHasSingleWinner(t *testing.T) {
	host := newSharedStateHost(t)
	service := New()
	ctx := testCallContext("tenant-a")
	result, _, _ := service.Handler(context.Background(), host, ctx, []byte(`{"key":"theme","value":"light","ifVersion":0}`), "put")
	if result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatal(result)
	}
	start := make(chan struct{})
	results := make(chan *contractv1.CallResult, 2)
	for _, value := range []string{"dark", "system"} {
		go func(value string) {
			<-start
			payload := []byte(`{"key":"theme","value":"` + value + `","ifVersion":1}`)
			result, _, _ := New().Handler(context.Background(), host, ctx, payload, "put")
			results <- result
		}(value)
	}
	close(start)
	first, second := <-results, <-results
	statuses := []contractv1.CallResult_Status{first.GetStatus(), second.GetStatus()}
	if statuses[0] == statuses[1] {
		t.Fatalf("并发 CAS 必须一成一败: first=%+v second=%+v", first, second)
	}
	failed := first
	if failed.GetStatus() == contractv1.CallResult_STATUS_OK {
		failed = second
	}
	if failed.GetError().GetCode() != "platform.settings.version_conflict" || !failed.GetError().GetRetryable() {
		t.Fatalf("失败方必须返回可重试业务冲突: %+v", failed)
	}
}

type recordedCall struct {
	capability string
	payload    []byte
}

type sharedStateHost struct {
	store sharedstate.Store
	mu    sync.Mutex
	calls []recordedCall
}

var _ sdk.Host = (*sharedStateHost)(nil)

func newSharedStateHost(t *testing.T) *sharedStateHost {
	store, err := sharedstate.OpenFileStore(filepath.Join(t.TempDir(), "shared.json"))
	if err != nil {
		t.Fatal(err)
	}
	return &sharedStateHost{store: store}
}

func (h *sharedStateHost) Call(ctx context.Context, target *contractv1.CallTarget, call *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
	h.mu.Lock()
	h.calls = append(h.calls, recordedCall{capability: target.GetCapability(), payload: append([]byte(nil), payload...)})
	h.mu.Unlock()
	operation := strings.TrimPrefix(target.GetCapability(), sharedstatev1.KernelServicePrefix)
	request, err := sharedstatev1.ParseRequest(operation, payload)
	if err != nil {
		return stateResult("state.invalid", false), nil, nil
	}
	scope := sharedstate.Scope{Kind: sharedstate.ScopeTenant, TenantID: call.GetTenantId(), PluginID: PluginID, RuntimeScope: "platform-settings", Namespace: stateNamespace}
	var response any
	switch typed := request.(type) {
	case *sharedstatev1.KeyRequest:
		response, err = h.store.Get(ctx, scope, typed.Key)
	case *sharedstatev1.WriteRequest:
		value, decodeErr := sharedstatev1.DecodeValue(typed.Value)
		if decodeErr != nil {
			err = decodeErr
		} else if operation == sharedstatev1.OperationCreate {
			response, err = h.store.Create(ctx, scope, typed.Key, value)
		} else {
			response, err = h.store.Update(ctx, scope, typed.Key, value, typed.ExpectedRevision)
		}
	}
	if err != nil {
		switch {
		case errors.Is(err, sharedstate.ErrNotFound):
			return stateResult("state.not_found", false), nil, nil
		case errors.Is(err, sharedstate.ErrConflict):
			return stateResult("state.conflict", true), nil, nil
		default:
			return stateResult("state.unavailable", true), nil, nil
		}
	}
	entry := response.(sharedstate.Entry)
	raw, _ := json.Marshal(sharedstatev1.Entry{Protocol: sharedstatev1.Protocol, Key: entry.Key, Value: sharedstatev1.EncodeValue(entry.Value), Revision: entry.Revision, UpdatedAt: entry.UpdatedAt})
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
}

func (h *sharedStateHost) callsSnapshot() []recordedCall {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]recordedCall(nil), h.calls...)
}

func stateResult(code string, retryable bool) *contractv1.CallResult {
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: code, Message: code, Retryable: retryable}}
}
