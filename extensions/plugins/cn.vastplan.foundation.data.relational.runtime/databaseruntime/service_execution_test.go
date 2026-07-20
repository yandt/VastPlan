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
	resolved databasev1.ConnectionSpec
	resolves int
	notFound bool
}

var _ sdk.Host = (*runtimeServiceHost)(nil)

func (h *runtimeServiceHost) Call(_ context.Context, target *contractv1.CallTarget, _ *contractv1.CallContext, _ []byte) (*contractv1.CallResult, []byte, error) {
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
	})
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
