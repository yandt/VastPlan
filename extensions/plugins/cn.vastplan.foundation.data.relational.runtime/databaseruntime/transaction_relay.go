package databaseruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"

	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

type transactionRelay struct {
	Operation   string                            `json:"operation"`
	Tenant      string                            `json:"tenant"`
	Project     string                            `json:"project,omitempty"`
	Caller      *contractv1.Caller                `json:"caller"`
	Credentials []*contractv1.CredentialRef       `json:"credentials,omitempty"`
	Query       *databasev1.QueryRequest          `json:"query,omitempty"`
	Execute     *databasev1.ExecuteRequest        `json:"execute,omitempty"`
	End         *databasev1.EndTransactionRequest `json:"end,omitempty"`
}

func (s *Service) proxyTransaction(ctx context.Context, host sdk.Host, call *contractv1.CallContext,
	operation, handle string, request any) (*contractv1.CallResult, []byte, error) {
	route, err := TransactionRoute(handle)
	if err != nil {
		return runtimeResult(nil, NewRuntimeError(databasev1.ErrorInvalidRequest, false, err))
	}
	if route == s.instanceID {
		return nil, nil, nil
	}
	if err := requireTransactionCaller(call); err != nil {
		return runtimeResult(nil, NewRuntimeError(databasev1.ErrorInvalidRequest, false, err))
	}
	relay := transactionRelay{Operation: operation, Tenant: call.GetTenantId(), Project: call.GetProjectId(), Caller: call.GetCaller(), Credentials: call.GetCredentials()}
	switch typed := request.(type) {
	case *databasev1.QueryRequest:
		if err := requireExecutor(call, typed.Connection); err != nil {
			return runtimeResult(nil, NewRuntimeError(databasev1.ErrorInvalidRequest, false, err))
		}
		relay.Query = typed
	case *databasev1.ExecuteRequest:
		if err := requireExecutor(call, typed.Connection); err != nil {
			return runtimeResult(nil, NewRuntimeError(databasev1.ErrorInvalidRequest, false, err))
		}
		relay.Execute = typed
	case *databasev1.EndTransactionRequest:
		relay.End = typed
	default:
		return runtimeResult(nil, NewRuntimeError(databasev1.ErrorInvalidRequest, false, errors.New("事务转发请求无效")))
	}
	payload, err := json.Marshal(relay)
	if err != nil {
		return nil, nil, err
	}
	proxyOperation, logicalService, routingDomain, instanceID := "transactionRelay", runtimeLogicalService, runtimeRoutingDomain, route
	result, raw, err := host.Call(ctx, &contractv1.CallTarget{
		ExtensionPoint: extpoint.ToolPackage, Capability: databasev1.Capability, Operation: &proxyOperation,
		LogicalService: &logicalService, RoutingDomain: &routingDomain, InstanceId: &instanceID,
	}, call, payload)
	if err != nil || result == nil {
		return runtimeResult(nil, NewRuntimeError(databasev1.ErrorTransactionLost, true, errors.New("事务所属 Runtime 实例已离线")))
	}
	return result, raw, nil
}

func (s *Service) relayTransaction(ctx context.Context, call *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
	if call.GetCaller().GetKind() != contractv1.CallerKind_CALLER_KIND_PLUGIN || call.GetCaller().GetId() != databasev1.RuntimePluginID {
		return runtimeResult(nil, NewRuntimeError(databasev1.ErrorInvalidRequest, false, errors.New("只有 Database Runtime 实例可以转发事务")))
	}
	var relay transactionRelay
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&relay); err != nil || relay.Caller == nil {
		return runtimeResult(nil, NewRuntimeError(databasev1.ErrorInvalidRequest, false, errors.New("事务转发信封无效")))
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return runtimeResult(nil, NewRuntimeError(databasev1.ErrorInvalidRequest, false, errors.New("事务转发信封无效")))
	}
	original := &contractv1.CallContext{TenantId: relay.Tenant, Caller: relay.Caller, Credentials: relay.Credentials}
	if relay.Project != "" {
		original.ProjectId = &relay.Project
	}
	if err := requireTransactionCaller(original); err != nil {
		return runtimeResult(nil, NewRuntimeError(databasev1.ErrorInvalidRequest, false, err))
	}
	switch relay.Operation {
	case databasev1.OperationQuery:
		if relay.Query == nil || relay.Execute != nil || relay.End != nil {
			return runtimeResult(nil, NewRuntimeError(databasev1.ErrorInvalidRequest, false, errors.New("事务查询转发信封无效")))
		}
		value, err := s.transactions.Query(ctx, original, relay.Query)
		return runtimeResult(value, err)
	case databasev1.OperationExecute:
		if relay.Execute == nil || relay.Query != nil || relay.End != nil {
			return runtimeResult(nil, NewRuntimeError(databasev1.ErrorInvalidRequest, false, errors.New("事务执行转发信封无效")))
		}
		value, err := s.transactions.Execute(ctx, original, relay.Execute)
		return runtimeResult(value, err)
	case databasev1.OperationCommit, databasev1.OperationRollback:
		if relay.End == nil || relay.Query != nil || relay.Execute != nil {
			return runtimeResult(nil, NewRuntimeError(databasev1.ErrorInvalidRequest, false, errors.New("事务结束转发信封无效")))
		}
		if relay.Operation == databasev1.OperationCommit {
			ref, err := s.transactions.Connection(relay.End.TransactionHandle, original)
			if err != nil {
				return runtimeResult(nil, err)
			}
			if err := requireExecutor(original, ref); err != nil {
				return runtimeResult(nil, NewRuntimeError(databasev1.ErrorInvalidRequest, false, err))
			}
		}
		err := s.transactions.End(ctx, original, relay.End.TransactionHandle, relay.Operation == databasev1.OperationCommit)
		return runtimeResult(struct{}{}, err)
	default:
		return runtimeResult(nil, NewRuntimeError(databasev1.ErrorInvalidRequest, false, errors.New("事务转发操作无效")))
	}
}
