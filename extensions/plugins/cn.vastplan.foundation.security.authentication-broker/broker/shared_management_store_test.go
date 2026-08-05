package broker

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	authenticationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authentication/v1"
	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
	sharedstatev1 "cdsoft.com.cn/VastPlan/contracts/schemas/sharedstate/v1"
)

type managementSharedStateHost struct {
	value       []byte
	revision    uint64
	unavailable bool
}

func (h *managementSharedStateHost) Call(_ context.Context, target *contractv1.CallTarget, _ *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
	if h.unavailable {
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: "state.unavailable", Retryable: true}}, nil, nil
	}
	operation := ""
	switch target.GetCapability() {
	case sharedstatev1.KernelService(sharedstatev1.OperationGet):
		if h.revision == 0 {
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: "state.not_found"}}, nil, nil
		}
		return managementSharedStateEntry(h.value, h.revision)
	case sharedstatev1.FencedKernelService(sharedstatev1.OperationCreate):
		operation = sharedstatev1.OperationCreate
	case sharedstatev1.FencedKernelService(sharedstatev1.OperationUpdate):
		operation = sharedstatev1.OperationUpdate
	default:
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: "unexpected_target"}}, nil, nil
	}
	parsed, err := sharedstatev1.ParseRequest(operation, payload)
	if err != nil {
		return nil, nil, err
	}
	request := parsed.(*sharedstatev1.WriteRequest)
	if operation == sharedstatev1.OperationCreate && h.revision != 0 || operation == sharedstatev1.OperationUpdate && request.ExpectedRevision != h.revision {
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: "state.conflict", Retryable: true}}, nil, nil
	}
	value, err := sharedstatev1.DecodeValue(request.Value)
	if err != nil {
		return nil, nil, err
	}
	h.value = append([]byte(nil), value...)
	h.revision++
	return managementSharedStateEntry(h.value, h.revision)
}

func TestBootstrapCatalogFallbackRejectsSharedStateOutage(t *testing.T) {
	call := &contractv1.CallContext{TenantId: "tenant-a", Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_USER, Id: "admin"}}
	bootstrap := authenticationv1.AuthenticationProviderCatalog{Document: compositioncommonv1.Document{Version: 1, ID: "seed", Revision: 1}, Providers: []authenticationv1.ProviderCatalogEntry{}, Bindings: []authenticationv1.ProviderBinding{}}
	root := BootstrapFallbackCatalog{Primary: StateCatalog{Store: &SharedManagementStore{}}, Bootstrap: staticCatalog{value: bootstrap}}
	if catalog, err := bindCatalog(context.Background(), root, &managementSharedStateHost{}, call).Load(); err != nil || catalog.ID != "seed" {
		t.Fatalf("仅未发布状态应使用 Bootstrap Catalog: %+v err=%v", catalog, err)
	}
	if _, err := bindCatalog(context.Background(), root, &managementSharedStateHost{unavailable: true}, call).Load(); err == nil {
		t.Fatal("Shared State 不可用时不得静默回退 Bootstrap Catalog")
	}
}

func managementSharedStateEntry(value []byte, revision uint64) (*contractv1.CallResult, []byte, error) {
	raw, err := json.Marshal(sharedstatev1.Entry{
		Protocol: sharedstatev1.Protocol, Key: managementStateKey, Value: sharedstatev1.EncodeValue(value),
		Revision: revision, UpdatedAt: time.Now().UTC(),
	})
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, err
}

func TestSharedManagementStorePersistsCASStateAndBindsCatalog(t *testing.T) {
	host := &managementSharedStateHost{}
	call := &contractv1.CallContext{TenantId: "tenant-a", Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_USER, Id: "admin"}}
	root := &SharedManagementStore{}
	store := root.Bind(context.Background(), host, call)
	initial, err := store.LoadState()
	if err != nil || initial.Generation != 0 || initial.Version != managementStateVersion {
		t.Fatalf("空 Shared State 应返回初始状态: %+v err=%v", initial, err)
	}
	catalog := authenticationv1.AuthenticationProviderCatalog{
		Document:  compositioncommonv1.Document{Version: 1, ID: "enterprise", Revision: 1},
		Providers: []authenticationv1.ProviderCatalogEntry{}, Bindings: []authenticationv1.ProviderBinding{},
	}
	next := initial
	next.Generation = 1
	next.Catalog = &catalog
	next.UpdatedAt = time.Now().UTC()
	if _, err := store.UpdateState(0, next); err != nil {
		t.Fatal(err)
	}
	restarted := root.Bind(context.Background(), host, call)
	loaded, err := restarted.LoadState()
	if err != nil || loaded.Generation != 1 || loaded.Catalog == nil || loaded.Catalog.ID != "enterprise" {
		t.Fatalf("Shared State 重启恢复失败: %+v err=%v", loaded, err)
	}
	stale := next
	stale.Catalog = nil
	if _, err := restarted.UpdateState(0, stale); err == nil {
		t.Fatal("旧 generation 必须触发 CAS 冲突")
	}
	boundCatalog := bindCatalog(context.Background(), StateCatalog{Store: root}, host, call)
	if published, err := boundCatalog.Load(); err != nil || published.ID != "enterprise" {
		t.Fatalf("Broker Catalog 必须绑定同一调用的 Shared State: %+v err=%v", published, err)
	}
}
