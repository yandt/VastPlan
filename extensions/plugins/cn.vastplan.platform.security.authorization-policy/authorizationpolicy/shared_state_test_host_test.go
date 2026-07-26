package authorizationpolicy

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	sharedstatev1 "cdsoft.com.cn/VastPlan/contracts/schemas/sharedstate/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/sharedstate"
)

type policyStateHost struct {
	store        sharedstate.Store
	available    bool
	capabilities []string
}

func newPolicyStateHost(t *testing.T) *policyStateHost {
	t.Helper()
	store, err := sharedstate.OpenFileStore(filepath.Join(t.TempDir(), "shared-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	return &policyStateHost{store: store, available: true}
}

func (h *policyStateHost) Call(ctx context.Context, target *contractv1.CallTarget, _ *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
	if !h.available {
		return nil, nil, errors.New("shared state unavailable")
	}
	capability := target.GetCapability()
	h.capabilities = append(h.capabilities, capability)
	operation := strings.TrimPrefix(capability, sharedstatev1.FencedKernelServicePrefix)
	operation = strings.TrimPrefix(operation, sharedstatev1.KernelServicePrefix)
	request, err := sharedstatev1.ParseRequest(operation, payload)
	if err != nil {
		return policyStateError("state.invalid", false), nil, nil
	}
	scope := sharedstate.Scope{Kind: sharedstate.ScopeService, PluginID: PluginID, RuntimeScope: "platform-authorization", Namespace: sharedStateNamespace}
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
			return policyStateError("state.not_found", false), nil, nil
		case errors.Is(err, sharedstate.ErrConflict):
			return policyStateError("state.conflict", true), nil, nil
		default:
			return policyStateError("state.unavailable", true), nil, nil
		}
	}
	entry := response.(sharedstate.Entry)
	raw, _ := json.Marshal(sharedstatev1.Entry{Protocol: sharedstatev1.Protocol, Key: entry.Key, Value: sharedstatev1.EncodeValue(entry.Value), Revision: entry.Revision, UpdatedAt: entry.UpdatedAt})
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
}

func policyStateError(code string, retryable bool) *contractv1.CallResult {
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: code, Message: code, Retryable: retryable}}
}

func policyUserContext() *contractv1.CallContext {
	return &contractv1.CallContext{TenantId: "local", Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_USER, Id: "owner"}, Principal: &contractv1.Principal{TenantId: "local", UserId: "owner"}}
}
