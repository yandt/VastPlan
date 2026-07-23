package portalcomposer

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	sharedstatev1 "cdsoft.com.cn/VastPlan/contracts/schemas/sharedstate/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/sharedstate"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

type stateOnlyHost struct{ store sharedstate.Store }

var _ sdk.Host = (*stateOnlyHost)(nil)

func newStateOnlyHost(t *testing.T) *stateOnlyHost {
	store, err := sharedstate.OpenFileStore(filepath.Join(t.TempDir(), "shared-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	return &stateOnlyHost{store: store}
}

func (h *stateOnlyHost) Call(ctx context.Context, target *contractv1.CallTarget, call *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
	operation := strings.TrimPrefix(target.GetCapability(), sharedstatev1.KernelServicePrefix)
	request, err := sharedstatev1.ParseRequest(operation, payload)
	if err != nil {
		return stateOnlyResult("state.invalid", false), nil, nil
	}
	var namespace string
	switch typed := request.(type) {
	case *sharedstatev1.KeyRequest:
		namespace = typed.Namespace
	case *sharedstatev1.WriteRequest:
		namespace = typed.Namespace
	}
	scope := sharedstate.Scope{Kind: sharedstate.ScopeTenant, TenantID: call.GetTenantId(), PluginID: PluginID, RuntimeScope: "portal-composer", Namespace: namespace}
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
			return stateOnlyResult("state.not_found", false), nil, nil
		case errors.Is(err, sharedstate.ErrConflict):
			return stateOnlyResult("state.conflict", true), nil, nil
		default:
			return stateOnlyResult("state.unavailable", true), nil, nil
		}
	}
	entry := response.(sharedstate.Entry)
	raw, _ := json.Marshal(sharedstatev1.Entry{Protocol: sharedstatev1.Protocol, Key: entry.Key, Value: sharedstatev1.EncodeValue(entry.Value), Revision: entry.Revision, UpdatedAt: entry.UpdatedAt})
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
}

func stateOnlyResult(code string, retryable bool) *contractv1.CallResult {
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: code, Message: code, Retryable: retryable}}
}
