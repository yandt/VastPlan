package apiexposure

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	sharedstatev1 "cdsoft.com.cn/VastPlan/contracts/schemas/sharedstate/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/sharedstate"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func TestAPIExposureSharedStateSurvivesLeaderReplacement(t *testing.T) {
	host := newAPIExposureStateHost(t)
	call := apiExposureUserCall("tenant-a", "alice", "platform.api-exposure.edit", "platform.api-exposure.read")
	first, err := New(filepath.Join(t.TempDir(), "gateway-a.json"), testContractCatalog())
	if err != nil {
		t.Fatal(err)
	}
	request, _ := json.Marshal(CreateDraftRequest{Contract: testContractSelector(), Input: testExposureInput()})
	result, _, err := Contribution(first).Handlers["createDraft"](context.Background(), host, call, request)
	if err != nil || result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("创建 Shared State 草稿失败: result=%+v err=%v", result, err)
	}

	second, err := New(filepath.Join(t.TempDir(), "gateway-b.json"), testContractCatalog())
	if err != nil {
		t.Fatal(err)
	}
	result, raw, err := Contribution(second).Handlers["list"](context.Background(), host, call, []byte(`{}`))
	if err != nil || result.GetStatus() != contractv1.CallResult_STATUS_OK || !strings.Contains(string(raw), `"tenantId":"tenant-a"`) {
		t.Fatalf("替换 leader 未恢复治理状态: result=%+v raw=%s err=%v", result, raw, err)
	}
}

func TestAPIExposureSharedStateCASRejectsStaleLeader(t *testing.T) {
	host := newAPIExposureStateHost(t)
	call := apiExposureUserCall("tenant-a", "alice")
	repository, _ := newAPIExposureStateRepository(host)
	state, revision, err := repository.load(context.Background(), call)
	if err != nil || revision != 0 {
		t.Fatal(err)
	}
	revision, err = repository.save(context.Background(), call, state, 0)
	if err != nil {
		t.Fatal(err)
	}
	first, firstRevision, _ := repository.load(context.Background(), call)
	second, secondRevision, _ := repository.load(context.Background(), call)
	first.NextRevision = 1
	if _, err := repository.save(context.Background(), call, first, firstRevision); err != nil {
		t.Fatal(err)
	}
	second.NextRevision = 2
	if _, err := repository.save(context.Background(), call, second, secondRevision); !errors.Is(err, ErrStoreConflict) {
		t.Fatalf("陈旧 leader 必须被 Shared State CAS 拒绝: %v (seed revision=%d)", err, revision)
	}
}

func TestAPIExposureFailsClosedWithoutSharedState(t *testing.T) {
	service, err := New(filepath.Join(t.TempDir(), "gateway.json"), testContractCatalog())
	if err != nil {
		t.Fatal(err)
	}
	host := &apiExposureStateHost{unavailable: true}
	result, _, err := Contribution(service).Handlers["list"](context.Background(), host, apiExposureUserCall("tenant-a", "alice", "platform.api-exposure.read"), []byte(`{}`))
	if err != nil || result.GetError().GetCode() != "platform.api-exposure.unavailable" {
		t.Fatalf("Shared State 故障必须 fail-closed: result=%+v err=%v", result, err)
	}
}

type apiExposureStateHost struct {
	store       sharedstate.Store
	mu          sync.Mutex
	unavailable bool
}

var _ sdk.Host = (*apiExposureStateHost)(nil)

func newAPIExposureStateHost(t *testing.T) *apiExposureStateHost {
	store, err := sharedstate.OpenFileStore(filepath.Join(t.TempDir(), "shared-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	return &apiExposureStateHost{store: store}
}

func (h *apiExposureStateHost) Call(ctx context.Context, target *contractv1.CallTarget, _ *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.unavailable {
		return apiExposureStateResult("state.unavailable", true), nil, nil
	}
	operation := strings.TrimPrefix(target.GetCapability(), sharedstatev1.FencedKernelServicePrefix)
	operation = strings.TrimPrefix(operation, sharedstatev1.KernelServicePrefix)
	request, err := sharedstatev1.ParseRequest(operation, payload)
	if err != nil {
		return apiExposureStateResult("state.invalid", false), nil, nil
	}
	scope := sharedstate.Scope{Kind: sharedstate.ScopeService, PluginID: PluginID, RuntimeScope: "platform-api-exposure", Namespace: sharedStateNamespace}
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
			return apiExposureStateResult("state.not_found", false), nil, nil
		case errors.Is(err, sharedstate.ErrConflict):
			return apiExposureStateResult("state.conflict", true), nil, nil
		default:
			return apiExposureStateResult("state.unavailable", true), nil, nil
		}
	}
	entry := response.(sharedstate.Entry)
	raw, _ := json.Marshal(sharedstatev1.Entry{Protocol: sharedstatev1.Protocol, Key: entry.Key, Value: sharedstatev1.EncodeValue(entry.Value), Revision: entry.Revision, UpdatedAt: entry.UpdatedAt})
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
}

func apiExposureStateResult(code string, retryable bool) *contractv1.CallResult {
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: code, Message: code, Retryable: retryable}}
}

func apiExposureUserCall(tenant, subject string, roles ...string) *contractv1.CallContext {
	return &contractv1.CallContext{TenantId: tenant, Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_USER, Id: "portal"}, Principal: &contractv1.Principal{UserId: subject, SystemRoles: roles}}
}
