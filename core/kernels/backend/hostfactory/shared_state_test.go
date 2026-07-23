package hostfactory

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	sharedstatev1 "cdsoft.com.cn/VastPlan/contracts/schemas/sharedstate/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/observability"
	"cdsoft.com.cn/VastPlan/core/shared/go/runtimeidentity"
	"cdsoft.com.cn/VastPlan/core/shared/go/sharedstate"
)

type unavailableSharedStateStore struct{}

func (unavailableSharedStateStore) Get(context.Context, sharedstate.Scope, string) (sharedstate.Entry, error) {
	return sharedstate.Entry{}, errors.New("provider unavailable")
}
func (unavailableSharedStateStore) Create(context.Context, sharedstate.Scope, string, []byte) (sharedstate.Entry, error) {
	return sharedstate.Entry{}, errors.New("provider unavailable")
}
func (unavailableSharedStateStore) Update(context.Context, sharedstate.Scope, string, []byte, uint64) (sharedstate.Entry, error) {
	return sharedstate.Entry{}, errors.New("provider unavailable")
}
func (unavailableSharedStateStore) Delete(context.Context, sharedstate.Scope, string, uint64) error {
	return errors.New("provider unavailable")
}
func (unavailableSharedStateStore) List(context.Context, sharedstate.Scope, string, int, string) (sharedstate.Page, error) {
	return sharedstate.Page{}, errors.New("provider unavailable")
}

func TestKernelSharedStateDerivesIdentityAndKeepsTenantsIsolated(t *testing.T) {
	store, err := sharedstate.OpenFileStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	create := kernelSharedState(store, sharedstatev1.OperationCreate)
	get := kernelSharedState(store, sharedstatev1.OperationGet)
	identity := runtimeidentity.Identity{
		PluginID: "cn.vastplan.demo", Publisher: "vastplan", Version: "1.0.0", ArtifactSHA256: strings.Repeat("a", 64),
		NodeID: "node-a", RuntimeScope: "service-a", InstanceID: "instance-a",
	}
	trusted, err := runtimeidentity.WithIdentity(context.Background(), identity)
	if err != nil {
		t.Fatal(err)
	}
	call := &contractv1.CallContext{TenantId: "tenant-a", Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: identity.PluginID}}
	payload, _ := json.Marshal(sharedstatev1.WriteRequest{Scope: "tenant", Namespace: "settings", Key: "active", Value: sharedstatev1.EncodeValue([]byte(`{"ok":true}`))})
	result, raw, err := create(trusted, call, payload)
	if err != nil || result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("create: result=%+v err=%v", result, err)
	}
	entry, err := sharedstatev1.ParseEntry(raw)
	if err != nil || entry.Revision == 0 {
		t.Fatalf("entry=%+v err=%v", entry, err)
	}

	request, _ := json.Marshal(sharedstatev1.KeyRequest{Scope: "tenant", Namespace: "settings", Key: "active"})
	otherTenant := &contractv1.CallContext{TenantId: "tenant-b", Caller: call.Caller}
	result, _, err = get(trusted, otherTenant, request)
	if err != nil || result.GetError().GetCode() != "state.not_found" {
		t.Fatalf("跨 tenant 必须不可见: result=%+v err=%v", result, err)
	}

	otherIdentity := identity
	otherIdentity.PluginID = "cn.vastplan.other"
	otherIdentity.InstanceID = "instance-b"
	otherContext, _ := runtimeidentity.WithIdentity(context.Background(), otherIdentity)
	otherCaller := &contractv1.CallContext{TenantId: "tenant-a", Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: otherIdentity.PluginID}}
	result, _, err = get(otherContext, otherCaller, request)
	if err != nil || result.GetError().GetCode() != "state.not_found" {
		t.Fatalf("跨插件必须不可见: result=%+v err=%v", result, err)
	}
}

func TestKernelSharedStateRejectsMissingRuntimeIdentity(t *testing.T) {
	store, _ := sharedstate.OpenFileStore(filepath.Join(t.TempDir(), "state.json"))
	service := kernelSharedState(store, sharedstatev1.OperationGet)
	payload := []byte(`{"scope":"tenant","namespace":"settings","key":"active"}`)
	result, _, err := service(context.Background(), &contractv1.CallContext{TenantId: "tenant-a", Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: "cn.vastplan.demo"}}, payload)
	if err != nil || result.GetError().GetCode() != "state.identity_invalid" {
		t.Fatalf("缺少 host-only identity 必须 fail-closed: result=%+v err=%v", result, err)
	}
}

func TestKernelSharedStateMetricsUseFixedOperationAndOutcomeLabels(t *testing.T) {
	store, _ := sharedstate.OpenFileStore(filepath.Join(t.TempDir(), "state.json"))
	metrics := observability.NewMemoryMetrics()
	create := kernelSharedStateWithMetrics(store, sharedstatev1.OperationCreate, metrics)
	get := kernelSharedStateWithMetrics(store, sharedstatev1.OperationGet, metrics)
	identity := runtimeidentity.Identity{
		PluginID: "cn.vastplan.secret-plugin", Publisher: "vastplan", Version: "1.0.0", ArtifactSHA256: strings.Repeat("a", 64),
		NodeID: "node-a", RuntimeScope: "service-a", InstanceID: "instance-a",
	}
	trusted, _ := runtimeidentity.WithIdentity(context.Background(), identity)
	call := &contractv1.CallContext{TenantId: "secret-tenant", Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: identity.PluginID}}
	payload, _ := json.Marshal(sharedstatev1.WriteRequest{Scope: "tenant", Namespace: "settings", Key: "secret-key", Value: sharedstatev1.EncodeValue([]byte("secret-value"))})
	if result, _, _ := create(trusted, call, payload); result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatal("首次 create 应成功")
	}
	if result, _, _ := create(trusted, call, payload); result.GetError().GetCode() != "state.conflict" {
		t.Fatal("重复 create 应形成 conflict")
	}
	missing, _ := json.Marshal(sharedstatev1.KeyRequest{Scope: "tenant", Namespace: "settings", Key: "missing"})
	if result, _, _ := get(trusted, call, missing); result.GetError().GetCode() != "state.not_found" {
		t.Fatal("缺失读取应形成 not_found")
	}
	unavailable := kernelSharedStateWithMetrics(unavailableSharedStateStore{}, sharedstatev1.OperationGet, metrics)
	if result, _, _ := unavailable(trusted, call, missing); result.GetError().GetCode() != "state.unavailable" {
		t.Fatal("Provider 故障应形成 unavailable")
	}
	snapshot := metrics.Snapshot()
	if snapshot.Counters["shared_state_operations_total|operation=create|outcome=ok"] != 1 ||
		snapshot.Counters["shared_state_operations_total|operation=create|outcome=conflict"] != 1 ||
		snapshot.Counters["shared_state_operations_total|operation=get|outcome=not_found"] != 1 ||
		snapshot.Counters["shared_state_operations_total|operation=get|outcome=unavailable"] != 1 {
		t.Fatalf("Shared State 指标不完整: %+v", snapshot.Counters)
	}
	for key := range snapshot.Counters {
		for _, secret := range []string{"secret-tenant", "secret-plugin", "secret-key", "secret-value"} {
			if strings.Contains(key, secret) {
				t.Fatalf("Shared State 指标泄漏业务信息 %q: %s", secret, key)
			}
		}
	}
}
