package authorizationpolicy

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"strings"
	"testing"
	"time"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	authorizationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authorization/v1"
)

func TestSharedStateStoreSurvivesFactoryReopenAndRejectsStaleCAS(t *testing.T) {
	host := newPolicyStateHost(t)
	call := policyUserContext()
	first, err := SharedStateStoreFactory(context.Background(), host, call)
	if err != nil {
		t.Fatal(err)
	}
	state, err := first.Load()
	if err != nil || state.Generation != 0 {
		t.Fatalf("空 Shared State 应返回 generation 0: state=%+v err=%v", state, err)
	}
	state.Generation = 1
	if _, err := first.CompareAndSwap(0, state); err != nil {
		t.Fatal(err)
	}
	second, _ := SharedStateStoreFactory(context.Background(), host, call)
	reloaded, err := second.Load()
	if err != nil || reloaded.Generation != 1 {
		t.Fatalf("重新绑定 Store 后状态未保留: state=%+v err=%v", reloaded, err)
	}
	reloaded.Generation = 2
	if _, err := second.CompareAndSwap(1, reloaded); err != nil {
		t.Fatal(err)
	}
	state.Generation = 2
	if _, err := first.CompareAndSwap(1, state); err == nil || !strings.Contains(err.Error(), "CAS 冲突") {
		t.Fatalf("旧 generation 写入应冲突: %v", err)
	}
	for _, capability := range host.capabilities {
		if strings.HasSuffix(capability, ".create") || strings.HasSuffix(capability, ".update") {
			if !strings.HasPrefix(capability, "kernel.state.shared.fenced.") {
				t.Fatalf("写入未使用 leader fence: %s", capability)
			}
		}
	}
}

func TestServiceImportsBootstrapOnlyIntoEmptySharedState(t *testing.T) {
	catalog := testCatalog(t)
	profile := NativeProviderProfile(catalog)
	root, err := RootDomain(catalog, profile)
	if err != nil {
		t.Fatal(err)
	}
	bootstrap, err := BuildBootstrapState(catalog, profile, []authorizationv1.PolicyDomain{root}, []BootstrapGrant{{RoleID: "seed.owner", Title: "Owner", SubjectID: "owner", Permissions: []string{"platform.demo.read"}}}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	bootstrap.Generation = 8
	bootstrap.PolicyRevision = 3
	_, private, _ := ed25519.GenerateKey(rand.Reader)
	service, err := NewService(ServiceOptions{StoreFactory: SharedStateStoreFactory, BootstrapState: &bootstrap, Signer: Ed25519Signer{KeyID: "policy.1", Private: private}, SnapshotWriter: &memoryWriter{}, Catalog: catalog, ProviderProfile: profile, Domains: []authorizationv1.PolicyDomain{root}})
	if err != nil {
		t.Fatal(err)
	}
	host := newPolicyStateHost(t)
	result, raw, err := service.handle(context.Background(), host, policyUserContext(), "get", nil)
	if err != nil || result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("首次导入失败: result=%+v err=%v", result, err)
	}
	var imported State
	if json.Unmarshal(raw, &imported) != nil || imported.Generation != 1 || imported.PolicyRevision != 3 || len(imported.Roles) != 1 {
		t.Fatalf("首次导入未保留业务状态并重建 Store generation: %+v", imported)
	}
	bootstrap.Roles = nil
	result, raw, err = service.handle(context.Background(), host, policyUserContext(), "get", nil)
	if err != nil || result.GetStatus() != contractv1.CallResult_STATUS_OK || json.Unmarshal(raw, &imported) != nil || len(imported.Roles) != 1 {
		t.Fatalf("已有 Shared State 不应被本地 bootstrap 覆盖: result=%+v state=%+v err=%v", result, imported, err)
	}
}

func TestServiceNeverFallsBackWhenSharedStateUnavailable(t *testing.T) {
	catalog := testCatalog(t)
	profile := NativeProviderProfile(catalog)
	root, _ := RootDomain(catalog, profile)
	_, private, _ := ed25519.GenerateKey(rand.Reader)
	service, err := NewService(ServiceOptions{StoreFactory: SharedStateStoreFactory, BootstrapState: &State{Version: stateVersion, Generation: 9}, Signer: Ed25519Signer{KeyID: "policy.1", Private: private}, SnapshotWriter: &memoryWriter{}, Catalog: catalog, ProviderProfile: profile, Domains: []authorizationv1.PolicyDomain{root}})
	if err != nil {
		t.Fatal(err)
	}
	host := newPolicyStateHost(t)
	host.available = false
	result, _, err := service.handle(context.Background(), host, policyUserContext(), "get", nil)
	if err != nil || result.GetError().GetCode() != "platform.authorization.unavailable" {
		t.Fatalf("Shared State 故障必须 fail-closed: result=%+v err=%v", result, err)
	}
}
