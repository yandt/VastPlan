package pluginsettings

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	sharedstatev1 "cdsoft.com.cn/VastPlan/contracts/schemas/sharedstate/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/sharedstate"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func TestTenantStateIsSharedAcrossInstancesAndIsolated(t *testing.T) {
	host := newPluginSettingsStateHost(t)
	callA := sharedStateCall("tenant-a")
	first := New()
	if err := first.openStateSession(context.Background(), host, callA, "tenant-a"); err != nil {
		t.Fatal(err)
	}
	first.mu.Lock()
	first.tenantLocked("tenant-a").NextAudit = 7
	err := first.saveLocked()
	first.mu.Unlock()
	first.closeStateSession()
	if err != nil {
		t.Fatal(err)
	}

	second := New()
	if err := second.openStateSession(context.Background(), host, callA, "tenant-a"); err != nil {
		t.Fatal(err)
	}
	if got := second.state.Tenants["tenant-a"].NextAudit; got != 7 {
		t.Fatalf("第二实例未读到共享状态: got %d", got)
	}
	second.closeStateSession()
	if err := second.openStateSession(context.Background(), host, sharedStateCall("tenant-b"), "tenant-b"); err != nil {
		t.Fatal(err)
	}
	if got := second.state.Tenants["tenant-b"].NextAudit; got != 0 {
		t.Fatalf("tenant 状态泄漏: got %d", got)
	}
	second.closeStateSession()

	for _, payload := range host.payloadsSnapshot() {
		if strings.Contains(string(payload), "tenant-a") || strings.Contains(string(payload), PluginID) || strings.Contains(string(payload), "runtimeScope") {
			t.Fatalf("Shared State 请求泄漏可信身份: %s", payload)
		}
	}
}

func TestTenantStateConcurrentCASHasSingleWinner(t *testing.T) {
	host := newPluginSettingsStateHost(t)
	call := sharedStateCall("tenant-a")
	seed := New()
	if err := seed.openStateSession(context.Background(), host, call, "tenant-a"); err != nil {
		t.Fatal(err)
	}
	seed.mu.Lock()
	seed.tenantLocked("tenant-a").NextAudit = 1
	if err := seed.saveLocked(); err != nil {
		seed.mu.Unlock()
		t.Fatal(err)
	}
	seed.mu.Unlock()
	seed.closeStateSession()

	first, second := New(), New()
	if err := first.openStateSession(context.Background(), host, call, "tenant-a"); err != nil {
		t.Fatal(err)
	}
	if err := second.openStateSession(context.Background(), host, call, "tenant-a"); err != nil {
		t.Fatal(err)
	}
	first.mu.Lock()
	first.tenantLocked("tenant-a").NextAudit = 2
	firstErr := first.saveLocked()
	first.mu.Unlock()
	second.mu.Lock()
	second.tenantLocked("tenant-a").NextAudit = 3
	secondErr := second.saveLocked()
	second.mu.Unlock()
	first.closeStateSession()
	second.closeStateSession()
	if firstErr != nil || !errors.Is(secondErr, ErrConflict) {
		t.Fatalf("并发 CAS 必须一成一败: first=%v second=%v", firstErr, secondErr)
	}
}

func TestHandlerFailsClosedWhenSharedStateIsUnavailable(t *testing.T) {
	host := newPluginSettingsStateHost(t)
	host.unavailable = true
	result, _, err := New().Handler(context.Background(), host, sharedStateCall("tenant-a"), []byte(`{}`), "listCandidates")
	if err != nil {
		t.Fatal(err)
	}
	if result.GetError().GetCode() != "platform.plugin_configuration.unavailable" || !result.GetError().GetRetryable() {
		t.Fatalf("Shared State 故障必须 fail-closed 并可重试: %+v", result)
	}
}

func sharedStateCall(tenant string) *contractv1.CallContext {
	return &contractv1.CallContext{
		TenantId:  tenant,
		Caller:    &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: "portal-bff"},
		Principal: &contractv1.Principal{UserId: "operator"},
	}
}

type pluginSettingsStateHost struct {
	store       sharedstate.Store
	mu          sync.Mutex
	payloads    [][]byte
	unavailable bool
}

var _ sdk.Host = (*pluginSettingsStateHost)(nil)

func newPluginSettingsStateHost(t *testing.T) *pluginSettingsStateHost {
	store, err := sharedstate.OpenFileStore(filepath.Join(t.TempDir(), "shared.json"))
	if err != nil {
		t.Fatal(err)
	}
	return &pluginSettingsStateHost{store: store}
}

func (h *pluginSettingsStateHost) Call(ctx context.Context, target *contractv1.CallTarget, call *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
	h.mu.Lock()
	h.payloads = append(h.payloads, append([]byte(nil), payload...))
	h.mu.Unlock()
	if h.unavailable {
		return pluginSettingsStateResult("state.unavailable", true), nil, nil
	}
	operation := strings.TrimPrefix(target.GetCapability(), sharedstatev1.KernelServicePrefix)
	request, err := sharedstatev1.ParseRequest(operation, payload)
	if err != nil {
		return pluginSettingsStateResult("state.invalid", false), nil, nil
	}
	scope := sharedstate.Scope{Kind: sharedstate.ScopeTenant, TenantID: call.GetTenantId(), PluginID: PluginID, RuntimeScope: "plugin-settings", Namespace: stateNamespace}
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
			return pluginSettingsStateResult("state.not_found", false), nil, nil
		case errors.Is(err, sharedstate.ErrConflict):
			return pluginSettingsStateResult("state.conflict", true), nil, nil
		default:
			return pluginSettingsStateResult("state.unavailable", true), nil, nil
		}
	}
	entry := response.(sharedstate.Entry)
	raw, _ := json.Marshal(sharedstatev1.Entry{Protocol: sharedstatev1.Protocol, Key: entry.Key, Value: sharedstatev1.EncodeValue(entry.Value), Revision: entry.Revision, UpdatedAt: entry.UpdatedAt})
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
}

func (h *pluginSettingsStateHost) payloadsSnapshot() [][]byte {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([][]byte, len(h.payloads))
	for index := range h.payloads {
		out[index] = append([]byte(nil), h.payloads[index]...)
	}
	return out
}

func pluginSettingsStateResult(code string, retryable bool) *contractv1.CallResult {
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: code, Message: code, Retryable: retryable}}
}
