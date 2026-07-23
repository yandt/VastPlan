package hostfactory

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	sharedstatev1 "cdsoft.com.cn/VastPlan/contracts/schemas/sharedstate/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/runtimeidentity"
	"cdsoft.com.cn/VastPlan/core/shared/go/sharedstate"
)

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
