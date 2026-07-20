package databaseruntime

import (
	"errors"
	"sort"
	"time"

	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
)

// Metrics returns the portable, low-cardinality Database Runtime metrics view.
// Per-connection diagnostics remain in PoolManager.Snapshot and are never
// emitted as monitoring labels because even hashed connection IDs are still
// unbounded over a long-lived tenant fleet.
func (s *Service) Metrics() (databasev1.RuntimeMetricsResult, error) {
	if s == nil || s.manager == nil || s.transactions == nil {
		return databasev1.RuntimeMetricsResult{}, NewRuntimeError(databasev1.ErrorConnectionUnavailable, false,
			errors.New("Database Runtime service 未就绪"))
	}
	result := buildRuntimeMetrics(s.manager.Snapshot(), s.transactions.Snapshot(), time.Now().UTC())
	if err := databasev1.ValidateRuntimeMetricsResult(result); err != nil {
		return databasev1.RuntimeMetricsResult{}, err
	}
	return result, nil
}

func buildRuntimeMetrics(pools ManagerSnapshot, transactions TransactionSnapshot, observedAt time.Time) databasev1.RuntimeMetricsResult {
	result := databasev1.RuntimeMetricsResult{
		SchemaVersion: 1, ObservedAt: observedAt,
		Pools: databasev1.PoolMetrics{
			NodeReserved: uint64(pools.NodeReserved), BudgetRejected: pools.BudgetRejected,
			AcquireSucceeded: pools.AcquireSucceeded, AcquireTimeouts: pools.AcquireTimeouts,
			QueueRejected: pools.QueueRejected, ForcedDrains: pools.ForcedDrains, CloseFailures: pools.CloseFailures,
		},
		Transactions: databasev1.TransactionMetrics{
			Active: transactions.Active, Capacity: transactions.Capacity, Begins: transactions.Begins,
			Commits: transactions.Commits, Rollbacks: transactions.Rollbacks, Expired: transactions.Expired,
			Lost: transactions.Lost, Rejected: transactions.Rejected,
		},
		Samples: []databasev1.MetricSample{},
	}
	if result.Transactions.Capacity == 0 {
		result.Transactions.Capacity = 1
	}
	type aggregate struct {
		open, idle, inUse, maxOpen, waitCount, waitDurationMS uint64
	}
	byProvider := map[string]*aggregate{}
	for _, generation := range pools.Generations {
		if generation.State == PoolClosed {
			continue
		}
		result.Health.ActiveGenerations++
		if generation.State == PoolDraining {
			result.Health.DrainingGenerations++
		}
		if generation.CloseFailed {
			result.Health.CloseFailedGenerations++
		}
		if generation.Pool.Healthy {
			result.Health.HealthyGenerations++
		} else {
			result.Health.UnhealthyGenerations++
		}
		result.Pools.OpenConnections += nonNegativeInt64(generation.Pool.Open)
		result.Pools.IdleConnections += nonNegativeInt64(generation.Pool.Idle)
		result.Pools.InUseConnections += nonNegativeInt64(generation.Pool.InUse)
		result.Pools.MaxOpenConnections += nonNegativeInt64(generation.Pool.MaxOpen)
		result.Pools.Waiting += nonNegativeInt(generation.Waiting)
		result.Pools.InFlight += nonNegativeInt(generation.InFlight)
		result.Pools.WaitCount += nonNegativeInt64(generation.Pool.WaitCount)
		result.Pools.WaitDurationMS += nonNegativeInt64(generation.Pool.WaitDurationMS)
		provider := byProvider[generation.ProviderID]
		if provider == nil {
			provider = &aggregate{}
			byProvider[generation.ProviderID] = provider
		}
		provider.open += nonNegativeInt64(generation.Pool.Open)
		provider.idle += nonNegativeInt64(generation.Pool.Idle)
		provider.inUse += nonNegativeInt64(generation.Pool.InUse)
		provider.maxOpen += nonNegativeInt64(generation.Pool.MaxOpen)
		provider.waitCount += nonNegativeInt64(generation.Pool.WaitCount)
		provider.waitDurationMS += nonNegativeInt64(generation.Pool.WaitDurationMS)
	}
	switch {
	case result.Health.ActiveGenerations == 0:
		result.Health.Status = "idle"
	case result.Health.UnhealthyGenerations == 0 && result.Health.CloseFailedGenerations == 0:
		result.Health.Status = "ready"
	case result.Health.HealthyGenerations == 0:
		result.Health.Status = "unavailable"
	default:
		result.Health.Status = "degraded"
	}
	result.Samples = append(result.Samples, globalMetricSamples(result)...)
	providerIDs := make([]string, 0, len(byProvider))
	for providerID := range byProvider {
		providerIDs = append(providerIDs, providerID)
	}
	sort.Strings(providerIDs)
	for _, providerID := range providerIDs {
		values := byProvider[providerID]
		labels := map[string]string{"provider": providerID}
		result.Samples = append(result.Samples,
			gauge("vastplan_database_pool_open_connections", "connections", values.open, labels),
			gauge("vastplan_database_pool_idle_connections", "connections", values.idle, labels),
			gauge("vastplan_database_pool_in_use_connections", "connections", values.inUse, labels),
			gauge("vastplan_database_pool_max_open_connections", "connections", values.maxOpen, labels),
			counter("vastplan_database_pool_wait_total", "operations", values.waitCount, labels),
			counter("vastplan_database_pool_wait_duration_milliseconds_total", "milliseconds", values.waitDurationMS, labels),
		)
	}
	return result
}

func globalMetricSamples(result databasev1.RuntimeMetricsResult) []databasev1.MetricSample {
	return []databasev1.MetricSample{
		gauge("vastplan_database_runtime_pool_generations", "generations", result.Health.ActiveGenerations, nil),
		gauge("vastplan_database_runtime_pool_unhealthy_generations", "generations", result.Health.UnhealthyGenerations, nil),
		gauge("vastplan_database_runtime_pool_draining_generations", "generations", result.Health.DrainingGenerations, nil),
		gauge("vastplan_database_runtime_pool_close_failed_generations", "generations", result.Health.CloseFailedGenerations, nil),
		gauge("vastplan_database_runtime_pool_waiting", "operations", result.Pools.Waiting, nil),
		gauge("vastplan_database_runtime_pool_inflight", "operations", result.Pools.InFlight, nil),
		gauge("vastplan_database_runtime_pool_node_reserved", "connections", result.Pools.NodeReserved, nil),
		counter("vastplan_database_runtime_pool_budget_rejected_total", "operations", result.Pools.BudgetRejected, nil),
		counter("vastplan_database_runtime_pool_acquire_timeouts_total", "operations", result.Pools.AcquireTimeouts, nil),
		counter("vastplan_database_runtime_pool_queue_rejected_total", "operations", result.Pools.QueueRejected, nil),
		counter("vastplan_database_runtime_pool_forced_drains_total", "operations", result.Pools.ForcedDrains, nil),
		counter("vastplan_database_runtime_pool_close_failures_total", "operations", result.Pools.CloseFailures, nil),
		gauge("vastplan_database_runtime_transactions_active", "transactions", result.Transactions.Active, nil),
		gauge("vastplan_database_runtime_transactions_capacity", "transactions", result.Transactions.Capacity, nil),
		counter("vastplan_database_runtime_transactions_begins_total", "transactions", result.Transactions.Begins, nil),
		counter("vastplan_database_runtime_transactions_commits_total", "transactions", result.Transactions.Commits, nil),
		counter("vastplan_database_runtime_transactions_rollbacks_total", "transactions", result.Transactions.Rollbacks, nil),
		counter("vastplan_database_runtime_transactions_expired_total", "transactions", result.Transactions.Expired, nil),
		counter("vastplan_database_runtime_transactions_lost_total", "transactions", result.Transactions.Lost, nil),
		counter("vastplan_database_runtime_transactions_rejected_total", "transactions", result.Transactions.Rejected, nil),
	}
}

func gauge(name, unit string, value uint64, labels map[string]string) databasev1.MetricSample {
	return metric(name, "gauge", unit, value, labels)
}

func counter(name, unit string, value uint64, labels map[string]string) databasev1.MetricSample {
	return metric(name, "counter", unit, value, labels)
}

func metric(name, kind, unit string, value uint64, labels map[string]string) databasev1.MetricSample {
	var copied map[string]string
	if len(labels) > 0 {
		copied = make(map[string]string, len(labels))
		for key, item := range labels {
			copied[key] = item
		}
	}
	return databasev1.MetricSample{Name: name, Kind: kind, Unit: unit, Value: value, Labels: copied}
}

func nonNegativeInt(value int) uint64 {
	if value < 0 {
		return 0
	}
	return uint64(value)
}

func nonNegativeInt64(value int64) uint64 {
	if value < 0 {
		return 0
	}
	return uint64(value)
}
