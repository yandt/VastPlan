package hostfactory

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	sharedstatev1 "cdsoft.com.cn/VastPlan/contracts/schemas/sharedstate/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/observability"
	"cdsoft.com.cn/VastPlan/core/shared/go/operationfence"
	"cdsoft.com.cn/VastPlan/core/shared/go/protocolbus"
	"cdsoft.com.cn/VastPlan/core/shared/go/runtimeidentity"
	"cdsoft.com.cn/VastPlan/core/shared/go/sharedstate"
)

func kernelSharedState(store sharedstate.Store, operation string) protocolbus.HostService {
	return kernelSharedStateWithMetrics(store, operation, nil)
}

func kernelSharedStateWithMetrics(store sharedstate.Store, operation string, metrics observability.MetricSink) protocolbus.HostService {
	return kernelSharedStateService(store, operation, metrics, false)
}

func kernelFencedSharedStateWithMetrics(store sharedstate.Store, operation string, metrics observability.MetricSink) protocolbus.HostService {
	return kernelSharedStateService(store, operation, metrics, true)
}

func kernelSharedStateService(store sharedstate.Store, operation string, metrics observability.MetricSink, requireFence bool) protocolbus.HostService {
	return func(ctx context.Context, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
		started := time.Now()
		outcome := "internal_error"
		defer func() {
			if metrics != nil {
				labels := map[string]string{"operation": operation, "outcome": outcome}
				metrics.AddCounter("shared_state_operations_total", 1, labels)
				metrics.ObserveDuration("shared_state_operation_duration", time.Since(started), labels)
			}
		}()
		identity, ok := runtimeidentity.FromContext(ctx)
		if !ok || identity.Validate() != nil || callCtx.GetCaller().GetKind() != contractv1.CallerKind_CALLER_KIND_PLUGIN || callCtx.GetCaller().GetId() != identity.PluginID {
			outcome = "identity_rejected"
			return sharedStateError("state.identity_invalid", "Shared State 缺少可信 Runtime 身份", false), nil, nil
		}
		if requireFence {
			evidence, current := operationfence.FromContext(ctx)
			if !current || evidence.UnitID != identity.RuntimeScope {
				outcome = "fence_rejected"
				return sharedStateError("state.fence_invalid", "Shared State fenced mutation 缺少当前 Leader evidence", true), nil, nil
			}
		}
		request, err := sharedstatev1.ParseRequest(operation, payload)
		if err != nil {
			outcome = "invalid"
			return sharedStateError("state.invalid", "Shared State 请求无效", false), nil, nil
		}
		scopeName, namespace := sharedStateRequestScope(request)
		scope := sharedstate.Scope{Kind: sharedstate.ScopeKind(scopeName), PluginID: identity.PluginID, RuntimeScope: identity.RuntimeScope, Namespace: namespace}
		if scope.Kind == sharedstate.ScopeTenant {
			scope.TenantID = callCtx.GetTenantId()
		}
		if err := scope.Validate(); err != nil {
			outcome = "invalid"
			return sharedStateError("state.scope_invalid", "Shared State scope 无效", false), nil, nil
		}
		var response any
		switch typed := request.(type) {
		case *sharedstatev1.KeyRequest:
			response, err = store.Get(ctx, scope, typed.Key)
		case *sharedstatev1.WriteRequest:
			var value []byte
			value, err = sharedstatev1.DecodeValue(typed.Value)
			if err == nil && operation == sharedstatev1.OperationCreate {
				response, err = store.Create(ctx, scope, typed.Key, value)
			} else if err == nil {
				response, err = store.Update(ctx, scope, typed.Key, value, typed.ExpectedRevision)
			}
		case *sharedstatev1.DeleteRequest:
			err = store.Delete(ctx, scope, typed.Key, typed.ExpectedRevision)
			response = map[string]any{"protocol": sharedstatev1.Protocol}
		case *sharedstatev1.ListRequest:
			response, err = store.List(ctx, scope, typed.Prefix, typed.Limit, typed.PageCursor)
		default:
			err = sharedstate.ErrInvalid
		}
		if err != nil {
			outcome = sharedStateMetricOutcome(err)
			return sharedStateStoreError(err), nil, nil
		}
		raw, err := marshalSharedStateResponse(response)
		if err != nil {
			outcome = "encoding_error"
			return nil, nil, err
		}
		outcome = "ok"
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
	}
}

func sharedStateMetricOutcome(err error) string {
	switch {
	case errors.Is(err, sharedstate.ErrNotFound):
		return "not_found"
	case errors.Is(err, sharedstate.ErrConflict):
		return "conflict"
	case errors.Is(err, sharedstate.ErrInvalid):
		return "invalid"
	default:
		return "unavailable"
	}
}

func sharedStateRequestScope(request any) (string, string) {
	switch typed := request.(type) {
	case *sharedstatev1.KeyRequest:
		return typed.Scope, typed.Namespace
	case *sharedstatev1.WriteRequest:
		return typed.Scope, typed.Namespace
	case *sharedstatev1.DeleteRequest:
		return typed.Scope, typed.Namespace
	case *sharedstatev1.ListRequest:
		return typed.Scope, typed.Namespace
	default:
		return "", ""
	}
}

func marshalSharedStateResponse(value any) ([]byte, error) {
	switch typed := value.(type) {
	case sharedstate.Entry:
		return json.Marshal(sharedstatev1.Entry{Protocol: sharedstatev1.Protocol, Key: typed.Key, Value: sharedstatev1.EncodeValue(typed.Value), Revision: typed.Revision, UpdatedAt: typed.UpdatedAt})
	case sharedstate.Page:
		items := make([]sharedstatev1.Entry, 0, len(typed.Items))
		for _, item := range typed.Items {
			items = append(items, sharedstatev1.Entry{Protocol: sharedstatev1.Protocol, Key: item.Key, Value: sharedstatev1.EncodeValue(item.Value), Revision: item.Revision, UpdatedAt: item.UpdatedAt})
		}
		return json.Marshal(sharedstatev1.Page{Protocol: sharedstatev1.Protocol, Items: items, NextPageCursor: typed.NextCursor})
	default:
		return json.Marshal(value)
	}
}

func sharedStateStoreError(err error) *contractv1.CallResult {
	switch {
	case errors.Is(err, sharedstate.ErrNotFound):
		return sharedStateError("state.not_found", "Shared State 条目不存在", false)
	case errors.Is(err, sharedstate.ErrConflict):
		return sharedStateError("state.conflict", "Shared State revision 冲突", true)
	case errors.Is(err, sharedstate.ErrInvalid):
		return sharedStateError("state.invalid", "Shared State 请求无效", false)
	default:
		return sharedStateError("state.unavailable", "Shared State Provider 不可用", true)
	}
}

func sharedStateError(code, message string, retryable bool) *contractv1.CallResult {
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: code, Message: message, Retryable: retryable}}
}
