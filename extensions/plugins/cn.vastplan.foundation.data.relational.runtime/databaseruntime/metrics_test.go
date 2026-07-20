package databaseruntime

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
)

func TestRuntimeMetricsExposeBoundedPoolAndTransactionSignals(t *testing.T) {
	service, spec := newExecutionService(t)
	host := &runtimeServiceHost{}
	result, _ := invokeRuntime(t, service, host, databasev1.OperationActivate, managerCall(), databasev1.ActivateRequest{Connection: spec})
	if result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatal(result)
	}
	begin := beginTestTransaction(t, service, host, spec, 1_000)
	result, _ = invokeRuntime(t, service, host, databasev1.OperationCommit, executorCall(spec.Ref, true), databasev1.EndTransactionRequest{TransactionHandle: begin.TransactionHandle})
	if result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("提交事务失败: %+v", result)
	}
	result, raw := invokeRuntime(t, service, host, databasev1.OperationMetrics, managerCall(), databasev1.MetricsRequest{})
	if result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("连接管理插件读取指标失败: %+v", result)
	}
	var metrics databasev1.RuntimeMetricsResult
	if err := json.Unmarshal(raw, &metrics); err != nil {
		t.Fatal(err)
	}
	if err := databasev1.ValidateRuntimeMetricsResult(metrics); err != nil {
		t.Fatalf("Runtime metrics 不符合稳定契约: %v", err)
	}
	if metrics.SchemaVersion != 1 || metrics.Health.Status != "ready" || metrics.Pools.OpenConnections == 0 ||
		metrics.Transactions.Capacity == 0 || metrics.Transactions.Begins != 1 || metrics.Transactions.Commits != 1 {
		t.Fatalf("Runtime metrics 摘要错误: %+v", metrics)
	}
	if !hasMetric(metrics.Samples, "vastplan_database_runtime_transactions_commits_total", "counter") ||
		!hasMetric(metrics.Samples, "vastplan_database_pool_open_connections", "gauge") {
		t.Fatalf("缺少标准指标样本: %+v", metrics.Samples)
	}
	encoded := string(raw)
	for _, forbidden := range []string{"tenant-a", "orders.primary", "credential://", "test-password", "runtime-test-a"} {
		if strings.Contains(encoded, forbidden) {
			t.Fatalf("指标输出泄露标识或秘密 %q: %s", forbidden, encoded)
		}
	}
}

func TestRuntimeMetricsRejectTenantAndBusinessCallers(t *testing.T) {
	service, spec := newExecutionService(t)
	host := &runtimeServiceHost{}
	result, _ := invokeRuntime(t, service, host, databasev1.OperationMetrics, executorCall(spec.Ref, true), databasev1.MetricsRequest{})
	if result.GetError().GetCode() != databasev1.ErrorInvalidRequest {
		t.Fatalf("普通业务插件不得读取 Runtime 指标: %+v", result)
	}
	system := &contractv1.CallContext{Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_SYSTEM, Id: "collector"}}
	result, _ = invokeRuntime(t, service, host, databasev1.OperationMetrics, system, databasev1.MetricsRequest{})
	if result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("系统采集器应读取 Runtime 指标: %+v", result)
	}
}

func TestBuildRuntimeMetricsReportsUnavailableWithoutHealthyGeneration(t *testing.T) {
	snapshot := ManagerSnapshot{Generations: []GenerationSnapshot{{
		ProviderID: "postgresql", State: PoolReady, Pool: PoolStats{Open: 1, MaxOpen: 4, Healthy: false},
	}}}
	metrics := buildRuntimeMetrics(snapshot, TransactionSnapshot{Capacity: 4}, timeForMetricsTest())
	if metrics.Health.Status != "unavailable" || metrics.Health.UnhealthyGenerations != 1 {
		t.Fatalf("无健康 generation 必须报告 unavailable: %+v", metrics.Health)
	}
}

func hasMetric(samples []databasev1.MetricSample, name, kind string) bool {
	for _, sample := range samples {
		if sample.Name == name && sample.Kind == kind {
			return true
		}
	}
	return false
}

func timeForMetricsTest() time.Time { return time.Unix(1_784_000_000, 0).UTC() }
