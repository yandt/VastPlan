package databaseruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/extpoint"
	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
	recordstorev1 "cdsoft.com.cn/VastPlan/contracts/schemas/recordstore/v1"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

type recordTransactionRelay struct {
	Operation   string                      `json:"operation"`
	Tenant      string                      `json:"tenant"`
	Project     string                      `json:"project,omitempty"`
	Caller      *contractv1.Caller          `json:"caller"`
	Credentials []*contractv1.CredentialRef `json:"credentials,omitempty"`
	Payload     json.RawMessage             `json:"payload"`
}

func (s *Service) proxyRecordTransaction(ctx context.Context, host sdk.Host, call *contractv1.CallContext,
	operation, handle string, payload []byte) (*contractv1.CallResult, []byte, error) {
	route, err := TransactionRoute(handle)
	if err != nil {
		return recordResult(nil, NewRuntimeError(databasev1.ErrorInvalidRequest, false, err))
	}
	if route == s.instanceID {
		return nil, nil, nil
	}
	if err := requireTransactionCaller(call); err != nil {
		return recordResult(nil, NewRuntimeError(databasev1.ErrorInvalidRequest, false, err))
	}
	relay := recordTransactionRelay{
		Operation: operation, Tenant: call.GetTenantId(), Project: call.GetProjectId(),
		Caller: call.GetCaller(), Credentials: call.GetCredentials(), Payload: append(json.RawMessage(nil), payload...),
	}
	raw, err := json.Marshal(relay)
	if err != nil {
		return nil, nil, err
	}
	proxyOperation, logicalService, routingDomain, instanceID := "recordTransactionRelay", runtimeLogicalService, runtimeRoutingDomain, route
	result, response, err := host.Call(ctx, &contractv1.CallTarget{
		ExtensionPoint: extpoint.ToolPackage, Capability: recordstorev1.Capability, Operation: &proxyOperation,
		LogicalService: &logicalService, RoutingDomain: &routingDomain, InstanceId: &instanceID,
	}, call, raw)
	if err != nil || result == nil {
		return recordResult(nil, NewRuntimeError(databasev1.ErrorTransactionLost, true, errors.New("事务所属 Runtime 实例已离线")))
	}
	return result, response, nil
}

func (s *Service) recordTransactionRelayHandler() sdk.Handler {
	return func(ctx context.Context, host sdk.Host, call *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
		if call.GetCaller().GetKind() != contractv1.CallerKind_CALLER_KIND_PLUGIN || call.GetCaller().GetId() != databasev1.RuntimePluginID {
			return recordResult(nil, NewRuntimeError(databasev1.ErrorInvalidRequest, false, errors.New("只有 Database Runtime 实例可以转发 Record Store 事务")))
		}
		var relay recordTransactionRelay
		decoder := json.NewDecoder(bytes.NewReader(payload))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&relay); err != nil || relay.Caller == nil || len(relay.Payload) == 0 {
			return recordResult(nil, NewRuntimeError(databasev1.ErrorInvalidRequest, false, errors.New("Record Store 事务转发信封无效")))
		}
		var trailing any
		if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
			return recordResult(nil, NewRuntimeError(databasev1.ErrorInvalidRequest, false, errors.New("Record Store 事务转发信封无效")))
		}
		original := &contractv1.CallContext{TenantId: relay.Tenant, Caller: relay.Caller, Credentials: relay.Credentials}
		if relay.Project != "" {
			original.ProjectId = &relay.Project
		}
		switch relay.Operation {
		case recordstorev1.OperationCreate, recordstorev1.OperationGet, recordstorev1.OperationList,
			recordstorev1.OperationUpdate, recordstorev1.OperationDelete, recordstorev1.OperationBatch,
			recordstorev1.OperationCommit, recordstorev1.OperationRollback, recordstorev1.OperationAppendOutbox:
			return s.recordHandler(relay.Operation)(ctx, host, original, relay.Payload)
		default:
			return recordResult(nil, NewRuntimeError(databasev1.ErrorInvalidRequest, false, errors.New("Record Store 事务转发操作无效")))
		}
	}
}
