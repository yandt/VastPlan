package sqlsharedstate

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/extpoint"
	sharedstatesqlv1 "cdsoft.com.cn/VastPlan/contracts/schemas/sharedstatesql/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/sharedstate"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

type CapabilityService struct{ store sharedstate.Store }

func NewCapabilityService(store sharedstate.Store) (*CapabilityService, error) {
	if store == nil {
		return nil, errors.New("SQL Shared State Store 不能为空")
	}
	return &CapabilityService{store: store}, nil
}

func (s *CapabilityService) Contribution() sdk.Contribution {
	handlers := map[string]sdk.Handler{}
	for _, operation := range []string{sharedstatesqlv1.OperationGet, sharedstatesqlv1.OperationCreate, sharedstatesqlv1.OperationUpdate, sharedstatesqlv1.OperationDelete, sharedstatesqlv1.OperationList} {
		handlers[operation] = s.handler(operation)
	}
	return sdk.Contribution{ExtensionPoint: extpoint.ToolPackage, ID: sharedstatesqlv1.Capability,
		Descriptor: []byte(`{"title":"SQL Shared State","subcommands":[{"name":"get","description":"读取 Shared State 条目"},{"name":"create","description":"创建 Shared State 条目"},{"name":"update","description":"按 CAS 更新 Shared State 条目"},{"name":"delete","description":"按 CAS 删除 Shared State 条目"},{"name":"list","description":"分页列出 Shared State 条目"}]}`), Handlers: handlers}
}

func (s *CapabilityService) handler(operation string) sdk.Handler {
	return func(ctx context.Context, _ sdk.Host, call *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
		if call.GetCaller().GetKind() != contractv1.CallerKind_CALLER_KIND_SYSTEM {
			return sqlStateResult(nil, sharedstate.ErrInvalid)
		}
		parsed, err := sharedstatesqlv1.ParseRequest(operation, payload)
		if err != nil {
			return sqlStateResult(nil, sharedstate.ErrInvalid)
		}
		switch request := parsed.(type) {
		case *sharedstatesqlv1.KeyRequest:
			entry, callErr := s.store.Get(ctx, scopeFromWire(request.Scope), request.Key)
			return sqlStateResult(entryToWire(entry), callErr)
		case *sharedstatesqlv1.WriteRequest:
			value, _ := base64.StdEncoding.DecodeString(request.ValueBase64)
			var entry sharedstate.Entry
			var callErr error
			if operation == sharedstatesqlv1.OperationCreate {
				entry, callErr = s.store.Create(ctx, scopeFromWire(request.Scope), request.Key, value)
			} else {
				entry, callErr = s.store.Update(ctx, scopeFromWire(request.Scope), request.Key, value, request.ExpectedRevision)
			}
			return sqlStateResult(entryToWire(entry), callErr)
		case *sharedstatesqlv1.DeleteRequest:
			callErr := s.store.Delete(ctx, scopeFromWire(request.Scope), request.Key, request.ExpectedRevision)
			return sqlStateResult(sharedstatesqlv1.Ack{}, callErr)
		case *sharedstatesqlv1.ListRequest:
			page, callErr := s.store.List(ctx, scopeFromWire(request.Scope), request.Prefix, request.Limit, request.Cursor)
			return sqlStateResult(pageToWire(page), callErr)
		default:
			return sqlStateResult(nil, sharedstate.ErrInvalid)
		}
	}
}

func scopeFromWire(value sharedstatesqlv1.Scope) sharedstate.Scope {
	return sharedstate.Scope{Kind: sharedstate.ScopeKind(value.Kind), TenantID: value.TenantID, PluginID: value.PluginID, RuntimeScope: value.RuntimeScope, Namespace: value.Namespace}
}

func entryToWire(value sharedstate.Entry) sharedstatesqlv1.Entry {
	return sharedstatesqlv1.Entry{Key: value.Key, ValueBase64: base64.StdEncoding.EncodeToString(value.Value), Revision: value.Revision, UpdatedAt: value.UpdatedAt}
}

func pageToWire(value sharedstate.Page) sharedstatesqlv1.Page {
	result := sharedstatesqlv1.Page{Items: make([]sharedstatesqlv1.Entry, 0, len(value.Items)), NextCursor: value.NextCursor}
	for _, entry := range value.Items {
		result.Items = append(result.Items, entryToWire(entry))
	}
	return result
}

func sqlStateResult(value any, err error) (*contractv1.CallResult, []byte, error) {
	if err != nil {
		code, retryable := sharedstatesqlv1.ErrorUnavailable, true
		switch {
		case errors.Is(err, sharedstate.ErrInvalid):
			code, retryable = sharedstatesqlv1.ErrorInvalid, false
		case errors.Is(err, sharedstate.ErrNotFound):
			code, retryable = sharedstatesqlv1.ErrorNotFound, false
		case errors.Is(err, sharedstate.ErrConflict):
			code, retryable = sharedstatesqlv1.ErrorConflict, false
		}
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: code, Message: "SQL Shared State 请求失败", Retryable: retryable}}, nil, nil
	}
	raw, marshalErr := json.Marshal(value)
	if marshalErr != nil {
		return nil, nil, marshalErr
	}
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
}
