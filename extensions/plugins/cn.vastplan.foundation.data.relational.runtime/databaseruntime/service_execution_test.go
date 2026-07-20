package databaseruntime

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	commonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/common/v1"
	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

type runtimeServiceHost struct {
	resolved         databasev1.ConnectionSpec
	resolves         int
	notFound         bool
	relay            *Service
	relayUnavailable bool
}

var _ sdk.Host = (*runtimeServiceHost)(nil)

func (h *runtimeServiceHost) Call(ctx context.Context, target *contractv1.CallTarget, _ *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
	if target.GetOperation() == "transactionRelay" {
		if h.relayUnavailable || h.relay == nil {
			return nil, nil, errors.New("runtime instance unavailable")
		}
		return h.relay.Contribution().Handlers["transactionRelay"](ctx, h, &contractv1.CallContext{
			Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: databasev1.RuntimePluginID},
		}, payload)
	}
	if target.GetCapability() != "platform.database" || target.GetOperation() != "resolveRuntime" || h.resolved.Ref.ResourceID == "" {
		if h.notFound && target.GetCapability() == "platform.database" && target.GetOperation() == "resolveRuntime" {
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: "platform.database.not_found"}}, nil, nil
		}
		return nil, nil, errors.New("unexpected host call")
	}
	h.resolves++
	raw, _ := json.Marshal(databasev1.ActivateRequest{Connection: h.resolved})
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
}

func newExecutionService(t *testing.T) (*Service, databasev1.ConnectionSpec) {
	t.Helper()
	registry := NewRegistry()
	if err := registry.Register(fakeProvider{id: "postgresql"}); err != nil {
		t.Fatal(err)
	}
	manager, err := NewPoolManager(registry, DefaultManagerPolicy())
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewServiceWithManager(registry, manager, func(sdk.Host, string, commonv1.ManagedCredentialRef) (MaterialSource, error) {
		return &testMaterialSource{value: []byte("test-password")}, nil
	}, ServiceOptions{InstanceID: "runtime-test-a"})
	if err != nil {
		t.Fatal(err)
	}
	return service, testConnectionSpec("postgresql")
}

func managerCall() *contractv1.CallContext {
	return &contractv1.CallContext{TenantId: "tenant-a", Caller: &contractv1.Caller{
		Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: databasev1.ConnectionManagerPluginID,
	}}
}

func executorCall(ref databasev1.ConnectionRef, granted bool) *contractv1.CallContext {
	call := &contractv1.CallContext{TenantId: "tenant-a", Caller: &contractv1.Caller{
		Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: "cn.vastplan.application.orders",
	}}
	if granted {
		scope := "tenant"
		call.Credentials = []*contractv1.CredentialRef{{Name: "database.connection/" + ref.ResourceID, Scope: &scope}}
	}
	return call
}

func invokeRuntime(t *testing.T, service *Service, host sdk.Host, operation string, call *contractv1.CallContext, request any) (*contractv1.CallResult, []byte) {
	t.Helper()
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	result, payload, err := service.Contribution().Handlers[operation](context.Background(), host, call, raw)
	if err != nil {
		t.Fatal(err)
	}
	return result, payload
}

func TestServicePublishesAndExecutesOnlyWithProjectedConnectionGrant(t *testing.T) {
	service, spec := newExecutionService(t)
	host := &runtimeServiceHost{}
	result, _ := invokeRuntime(t, service, host, databasev1.OperationActivate, managerCall(), databasev1.ActivateRequest{Connection: spec})
	if result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("连接发布失败: %+v", result)
	}
	statement := databasev1.Statement{SQL: "select id from orders", Parameters: []databasev1.Value{}}
	request := databasev1.QueryRequest{Connection: spec.Ref, Statement: statement, MaxRows: 100}
	denied, _ := invokeRuntime(t, service, host, databasev1.OperationQuery, executorCall(spec.Ref, false), request)
	if denied.GetStatus() != contractv1.CallResult_STATUS_ERROR || denied.GetError().GetCode() != databasev1.ErrorInvalidRequest {
		t.Fatalf("没有宿主投影连接授权的调用必须拒绝: %+v", denied)
	}
	allowed, raw := invokeRuntime(t, service, host, databasev1.OperationQuery, executorCall(spec.Ref, true), request)
	if allowed.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("授权查询失败: %+v", allowed)
	}
	var query databasev1.QueryResult
	if json.Unmarshal(raw, &query) != nil || len(query.Rows) != 1 {
		t.Fatalf("查询响应无效: %s", raw)
	}
}

func TestServiceLazilyHydratesAnActiveActiveReplica(t *testing.T) {
	service, spec := newExecutionService(t)
	host := &runtimeServiceHost{resolved: spec}
	request := databasev1.ExecuteRequest{Connection: spec.Ref, Statement: databasev1.Statement{SQL: "update orders set ready = true", Parameters: []databasev1.Value{}}}
	result, raw := invokeRuntime(t, service, host, databasev1.OperationExecute, executorCall(spec.Ref, true), request)
	if result.GetStatus() != contractv1.CallResult_STATUS_OK || host.resolves != 1 || string(raw) != `{"rowsAffected":1}` {
		t.Fatalf("副本惰性收敛失败: result=%+v resolves=%d raw=%s", result, host.resolves, raw)
	}
}

func TestServiceStopsServingRemovedDefinitionAfterBoundedLease(t *testing.T) {
	service, spec := newExecutionService(t)
	host := &runtimeServiceHost{}
	result, _ := invokeRuntime(t, service, host, databasev1.OperationActivate, managerCall(), databasev1.ActivateRequest{Connection: spec})
	if result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatal(result)
	}
	host.notFound = true
	time.Sleep(definitionLeaseTTL + 20*time.Millisecond)
	request := databasev1.QueryRequest{Connection: spec.Ref, Statement: databasev1.Statement{SQL: "select 1", Parameters: []databasev1.Value{}}, MaxRows: 1}
	result, _ = invokeRuntime(t, service, host, databasev1.OperationQuery, executorCall(spec.Ref, true), request)
	if result.GetStatus() != contractv1.CallResult_STATUS_ERROR || result.GetError().GetCode() != databasev1.ErrorConnectionNotFound {
		t.Fatalf("定义删除后必须在有界 lease 内停止服务: %+v", result)
	}
}

func beginTestTransaction(t *testing.T, service *Service, host sdk.Host, spec databasev1.ConnectionSpec, timeoutMS int64) databasev1.BeginResult {
	t.Helper()
	result, raw := invokeRuntime(t, service, host, databasev1.OperationBegin, executorCall(spec.Ref, true), databasev1.BeginRequest{
		Connection: spec.Ref, Options: databasev1.TransactionOptions{Isolation: "read-committed", TimeoutMS: timeoutMS},
	})
	if result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("开始事务失败: %+v", result)
	}
	var begin databasev1.BeginResult
	if err := json.Unmarshal(raw, &begin); err != nil {
		t.Fatal(err)
	}
	return begin
}

func TestServiceTransactionLifecycleBindsCallerAndConnection(t *testing.T) {
	service, spec := newExecutionService(t)
	host := &runtimeServiceHost{}
	result, _ := invokeRuntime(t, service, host, databasev1.OperationActivate, managerCall(), databasev1.ActivateRequest{Connection: spec})
	if result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatal(result)
	}
	begin := beginTestTransaction(t, service, host, spec, 500)
	if route, err := TransactionRoute(begin.TransactionHandle); err != nil || route != "runtime-test-a" {
		t.Fatalf("事务句柄未绑定 Runtime 实例: route=%q err=%v", route, err)
	}
	query := databasev1.QueryRequest{Connection: spec.Ref, TransactionHandle: begin.TransactionHandle,
		Statement: databasev1.Statement{SQL: "select 1", Parameters: []databasev1.Value{}}, MaxRows: 1}
	result, _ = invokeRuntime(t, service, host, databasev1.OperationQuery, executorCall(spec.Ref, true), query)
	if result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("事务查询失败: %+v", result)
	}
	other := executorCall(spec.Ref, true)
	other.Caller.Id = "cn.vastplan.application.other"
	result, _ = invokeRuntime(t, service, host, databasev1.OperationQuery, other, query)
	if result.GetError().GetCode() != databasev1.ErrorInvalidRequest {
		t.Fatalf("事务句柄必须绑定原始 caller: %+v", result)
	}
	tampered := query
	replacement := "A"
	if begin.TransactionHandle[len(begin.TransactionHandle)-1:] == replacement {
		replacement = "B"
	}
	tampered.TransactionHandle = begin.TransactionHandle[:len(begin.TransactionHandle)-1] + replacement
	result, _ = invokeRuntime(t, service, host, databasev1.OperationQuery, executorCall(spec.Ref, true), tampered)
	if result.GetError().GetCode() != databasev1.ErrorInvalidRequest {
		t.Fatalf("篡改事务句柄必须 fail closed: %+v", result)
	}
	result, _ = invokeRuntime(t, service, host, databasev1.OperationCommit, executorCall(spec.Ref, true), databasev1.EndTransactionRequest{TransactionHandle: begin.TransactionHandle})
	if result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("提交事务失败: %+v", result)
	}
	result, _ = invokeRuntime(t, service, host, databasev1.OperationRollback, executorCall(spec.Ref, true), databasev1.EndTransactionRequest{TransactionHandle: begin.TransactionHandle})
	if result.GetError().GetCode() != databasev1.ErrorTransactionLost {
		t.Fatalf("已结束事务必须稳定返回 transaction_lost: %+v", result)
	}
	revoked := beginTestTransaction(t, service, host, spec, 500)
	result, _ = invokeRuntime(t, service, host, databasev1.OperationCommit, executorCall(spec.Ref, false), databasev1.EndTransactionRequest{TransactionHandle: revoked.TransactionHandle})
	if result.GetError().GetCode() != databasev1.ErrorInvalidRequest {
		t.Fatalf("提交时连接 grant 已撤销必须拒绝: %+v", result)
	}
	result, _ = invokeRuntime(t, service, host, databasev1.OperationRollback, executorCall(spec.Ref, false), databasev1.EndTransactionRequest{TransactionHandle: revoked.TransactionHandle})
	if result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("grant 撤销后仍必须允许原 caller 回滚: %+v", result)
	}
}

func TestServiceRelaysOpaqueTransactionToOwningReplicaAndReportsLoss(t *testing.T) {
	owner, spec := newExecutionService(t)
	other, _ := newExecutionService(t)
	other.instanceID = "runtime-test-b"
	other.transactions, _ = NewTransactionManager(other.instanceID, 0)
	ownerHost := &runtimeServiceHost{}
	result, _ := invokeRuntime(t, owner, ownerHost, databasev1.OperationActivate, managerCall(), databasev1.ActivateRequest{Connection: spec})
	if result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatal(result)
	}
	begin := beginTestTransaction(t, owner, ownerHost, spec, 500)
	query := databasev1.QueryRequest{Connection: spec.Ref, TransactionHandle: begin.TransactionHandle,
		Statement: databasev1.Statement{SQL: "select 1", Parameters: []databasev1.Value{}}, MaxRows: 1}
	relayHost := &runtimeServiceHost{relay: owner}
	result, _ = invokeRuntime(t, other, relayHost, databasev1.OperationQuery, executorCall(spec.Ref, true), query)
	if result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("非 owner 副本未透明转发事务: %+v", result)
	}
	relayHost.relayUnavailable = true
	result, _ = invokeRuntime(t, other, relayHost, databasev1.OperationQuery, executorCall(spec.Ref, true), query)
	if result.GetError().GetCode() != databasev1.ErrorTransactionLost || !result.GetError().GetRetryable() {
		t.Fatalf("owner 离线必须稳定返回 retryable transaction_lost: %+v", result)
	}
}

func TestServiceExpiresAndRollsBackTransaction(t *testing.T) {
	service, spec := newExecutionService(t)
	host := &runtimeServiceHost{}
	result, _ := invokeRuntime(t, service, host, databasev1.OperationActivate, managerCall(), databasev1.ActivateRequest{Connection: spec})
	if result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatal(result)
	}
	begin := beginTestTransaction(t, service, host, spec, 100)
	time.Sleep(130 * time.Millisecond)
	result, _ = invokeRuntime(t, service, host, databasev1.OperationCommit, executorCall(spec.Ref, true), databasev1.EndTransactionRequest{TransactionHandle: begin.TransactionHandle})
	if code := result.GetError().GetCode(); code != databasev1.ErrorTransactionExpired {
		t.Fatalf("超时事务必须稳定报告 transaction_expired 并已回滚: %+v", result)
	}
}

func TestCredentialRotationDrainsOldTransactionWithinBound(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(fakeProvider{id: "postgresql"}); err != nil {
		t.Fatal(err)
	}
	policy := DefaultManagerPolicy()
	policy.DrainTimeout = 25 * time.Millisecond
	manager, err := NewPoolManager(registry, policy)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewServiceWithManager(registry, manager, func(sdk.Host, string, commonv1.ManagedCredentialRef) (MaterialSource, error) {
		return &testMaterialSource{value: []byte("test-password")}, nil
	}, ServiceOptions{InstanceID: "runtime-rotation"})
	if err != nil {
		t.Fatal(err)
	}
	spec := testConnectionSpec("postgresql")
	host := &runtimeServiceHost{}
	result, _ := invokeRuntime(t, service, host, databasev1.OperationActivate, managerCall(), databasev1.ActivateRequest{Connection: spec})
	if result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatal(result)
	}
	begin := beginTestTransaction(t, service, host, spec, 500)
	rotated := spec
	rotated.Ref.Revision++
	rotated.Credentials.Version++
	result, _ = invokeRuntime(t, service, host, databasev1.OperationActivate, managerCall(), databasev1.ActivateRequest{Connection: rotated})
	if result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("轮换连接失败: %+v", result)
	}
	query := databasev1.QueryRequest{Connection: spec.Ref, TransactionHandle: begin.TransactionHandle,
		Statement: databasev1.Statement{SQL: "select 1", Parameters: []databasev1.Value{}}, MaxRows: 1}
	result, _ = invokeRuntime(t, service, host, databasev1.OperationQuery, executorCall(spec.Ref, true), query)
	if result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("drain 窗口内旧事务应可完成: %+v", result)
	}
	time.Sleep(60 * time.Millisecond)
	result, _ = invokeRuntime(t, service, host, databasev1.OperationQuery, executorCall(spec.Ref, true), query)
	if result.GetError().GetCode() != databasev1.ErrorTransactionLost {
		t.Fatalf("drain 超时后旧事务必须回滚并报告 transaction_lost: %+v", result)
	}
}

func TestServiceCloseRollsBackAllTransactions(t *testing.T) {
	service, spec := newExecutionService(t)
	host := &runtimeServiceHost{}
	result, _ := invokeRuntime(t, service, host, databasev1.OperationActivate, managerCall(), databasev1.ActivateRequest{Connection: spec})
	if result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatal(result)
	}
	begin := beginTestTransaction(t, service, host, spec, 500)
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}
	result, _ = invokeRuntime(t, service, host, databasev1.OperationCommit, executorCall(spec.Ref, true), databasev1.EndTransactionRequest{TransactionHandle: begin.TransactionHandle})
	if result.GetError().GetCode() != databasev1.ErrorTransactionLost {
		t.Fatalf("Runtime 关闭后事务必须回滚并报告 transaction_lost: %+v", result)
	}
}
