package databaseruntime

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
)

type managerProvider struct {
	mu                sync.Mutex
	pools             []*managerPool
	closeFailuresNext int
}

func (p *managerProvider) Descriptor() databasev1.ProviderDescriptor {
	return databasev1.ProviderDescriptor{
		ID: "postgresql", Version: "1.0.0", DisplayName: "PostgreSQL",
		ConfigurationSchema: json.RawMessage(`{"type":"object","additionalProperties":false}`),
		Capabilities:        databasev1.ProviderCapabilities{Query: true, Execute: true, Transactions: true},
	}
}

func (*managerProvider) Validate(_ context.Context, spec databasev1.ConnectionSpec) error {
	return databasev1.ValidateConnectionSpec(spec)
}

func (p *managerProvider) OpenPool(ctx context.Context, _ databasev1.ConnectionSpec, material MaterialSource) (Pool, error) {
	if err := material.WithMaterial(ctx, func(value CredentialMaterial) error {
		if len(value.Bytes()) == 0 {
			return errors.New("material 不能为空")
		}
		return nil
	}); err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	pool := &managerPool{healthy: true, closeFailures: p.closeFailuresNext}
	p.closeFailuresNext = 0
	p.pools = append(p.pools, pool)
	return pool, nil
}

func (p *managerProvider) poolCount() int { p.mu.Lock(); defer p.mu.Unlock(); return len(p.pools) }
func (p *managerProvider) pool(index int) *managerPool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.pools[index]
}

type managerPool struct {
	mu            sync.Mutex
	healthy       bool
	closed        bool
	closeFailures int
}

func (p *managerPool) Probe(context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.healthy {
		return errors.New("unhealthy")
	}
	return nil
}
func (p *managerPool) Query(context.Context, databasev1.Statement, int) (databasev1.QueryResult, error) {
	return testQueryResult(), nil
}
func (p *managerPool) Execute(context.Context, databasev1.Statement) (databasev1.ExecuteResult, error) {
	return databasev1.ExecuteResult{RowsAffected: 1}, nil
}
func (p *managerPool) Begin(context.Context, databasev1.TransactionOptions) (Transaction, error) {
	return &fakeTransaction{}, nil
}
func (p *managerPool) Stats() PoolStats {
	p.mu.Lock()
	defer p.mu.Unlock()
	return PoolStats{Open: 1, Idle: 1, MaxOpen: 1, Healthy: p.healthy && !p.closed}
}
func (p *managerPool) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closeFailures > 0 {
		p.closeFailures--
		return errors.New("close failed")
	}
	p.closed, p.healthy = true, false
	return nil
}
func (p *managerPool) isClosed() bool { p.mu.Lock(); defer p.mu.Unlock(); return p.closed }

func newManagerForTest(t *testing.T, policy ManagerPolicy) (*PoolManager, *managerProvider, *testMaterialSource) {
	t.Helper()
	provider := &managerProvider{}
	registry := NewRegistry()
	if err := registry.Register(provider); err != nil {
		t.Fatal(err)
	}
	manager, err := NewPoolManager(registry, policy)
	if err != nil {
		t.Fatal(err)
	}
	return manager, provider, &testMaterialSource{value: []byte("password")}
}

func managerScope(caller string) RequestScope {
	return RequestScope{TenantID: "tenant-a", ProjectID: "project-a", CallerID: caller}
}

func managerSpec(revision uint64, maxOpen int) databasev1.ConnectionSpec {
	spec := testConnectionSpec("postgresql")
	spec.Ref.Revision = revision
	spec.Credentials.Version = int64(revision)
	spec.Pool.MinIdle = 0
	spec.Pool.MaxIdle = maxOpen
	spec.Pool.MaxOpen = maxOpen
	spec.Pool.AcquireTimeoutMS = 100
	return spec
}

func waitFor(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("等待状态收敛超时")
}

func TestPoolManagerSwitchesGenerationAndDrainsInflight(t *testing.T) {
	manager, provider, material := newManagerForTest(t, ManagerPolicy{
		NodeMaxOpen: 32, TenantMaxOpen: 24, ConnectionMaxOpen: 16, MaxGenerations: 2,
		MaxWaitersPerPool: 8, MaxConcurrentPerCaller: 4, DrainTimeout: time.Second, ClosedHistoryLimit: 4,
	})
	scope := managerScope("plugin-a")
	first, err := manager.Activate(context.Background(), scope, managerSpec(1, 4), material)
	if err != nil {
		t.Fatal(err)
	}
	again, err := manager.Activate(context.Background(), scope, managerSpec(1, 4), material)
	if err != nil || again.Generation != first.Generation || provider.poolCount() != 1 {
		t.Fatalf("幂等 activate 失败: first=%+v again=%+v pools=%d err=%v", first, again, provider.poolCount(), err)
	}
	lease, err := manager.Acquire(context.Background(), scope, first.Connection)
	if err != nil || lease.Generation() != first.Generation {
		t.Fatalf("获取第一代连接失败: %v", err)
	}
	second, err := manager.Activate(context.Background(), scope, managerSpec(2, 4), material)
	if err != nil || second.Generation == first.Generation {
		t.Fatalf("generation 切换失败: %+v err=%v", second, err)
	}
	if provider.pool(0).isClosed() {
		t.Fatal("旧 generation 有在途 lease 时不得提前关闭")
	}
	if _, err := manager.Acquire(context.Background(), scope, first.Connection); err == nil {
		t.Fatal("切换后不得向旧 revision 分发新请求")
	}
	newLease, err := manager.Acquire(context.Background(), scope, second.Connection)
	if err != nil || newLease.Generation() != second.Generation {
		t.Fatalf("新 generation 不可用: %v", err)
	}
	newLease.Release()
	lease.Release()
	waitFor(t, time.Second, provider.pool(0).isClosed)
	snapshot := manager.Snapshot()
	if snapshot.NodeReserved != 4 || snapshot.Activations != 2 || snapshot.IdempotentActivations != 1 || len(snapshot.Generations) != 2 {
		t.Fatalf("generation 指标错误: %+v", snapshot)
	}
	raw, _ := json.Marshal(snapshot)
	if strings.Contains(string(raw), "tenant-a") || strings.Contains(string(raw), "orders.primary") || strings.Contains(string(raw), "credential://") {
		t.Fatalf("指标泄露敏感标识: %s", raw)
	}
	if err := manager.Retire(context.Background(), scope, second.Connection); err != nil {
		t.Fatal(err)
	}
	if err := manager.Retire(context.Background(), scope, second.Connection); err != nil {
		t.Fatalf("重复 retire 必须幂等: %v", err)
	}
	waitFor(t, time.Second, provider.pool(1).isClosed)
}

func TestPoolManagerRejectsBudgetBeforeOpeningProvider(t *testing.T) {
	manager, provider, material := newManagerForTest(t, ManagerPolicy{
		NodeMaxOpen: 20, TenantMaxOpen: 12, ConnectionMaxOpen: 6, MaxGenerations: 2,
		MaxWaitersPerPool: 4, MaxConcurrentPerCaller: 2, DrainTimeout: time.Second, ClosedHistoryLimit: 2,
	})
	scope := managerScope("plugin-a")
	if _, err := manager.Activate(context.Background(), scope, managerSpec(1, 4), material); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Activate(context.Background(), scope, managerSpec(2, 4), material); err == nil {
		t.Fatal("轮换重叠超过 connection 预算必须拒绝")
	} else if code, retryable := ErrorDetails(err); code != databasev1.ErrorPoolExhausted || !retryable {
		t.Fatalf("预算错误分类不稳定: code=%s retryable=%t", code, retryable)
	}
	if provider.poolCount() != 1 || material.calls != 1 {
		t.Fatalf("预算拒绝必须发生在 Provider/material 之前: pools=%d material=%d", provider.poolCount(), material.calls)
	}
	if manager.Snapshot().BudgetRejected != 1 {
		t.Fatal("预算拒绝指标未增加")
	}
}

func TestPoolManagerBoundsAcquireQueueAndCallerConcurrency(t *testing.T) {
	manager, _, material := newManagerForTest(t, ManagerPolicy{
		NodeMaxOpen: 20, TenantMaxOpen: 20, ConnectionMaxOpen: 10, MaxGenerations: 2,
		MaxWaitersPerPool: 1, MaxConcurrentPerCaller: 1, DrainTimeout: time.Second, ClosedHistoryLimit: 2,
	})
	spec := managerSpec(1, 1)
	result, err := manager.Activate(context.Background(), managerScope("plugin-a"), spec, material)
	if err != nil {
		t.Fatal(err)
	}
	first, err := manager.Acquire(context.Background(), managerScope("plugin-a"), result.Connection)
	if err != nil {
		t.Fatal(err)
	}
	waiterDone := make(chan error, 1)
	go func() {
		lease, acquireErr := manager.Acquire(context.Background(), managerScope("plugin-b"), result.Connection)
		if lease != nil {
			lease.Release()
		}
		waiterDone <- acquireErr
	}()
	waitFor(t, time.Second, func() bool { return manager.Snapshot().Generations[0].Waiting == 1 })
	if _, err := manager.Acquire(context.Background(), managerScope("plugin-c"), result.Connection); err == nil {
		t.Fatal("等待队列超过上限必须立即拒绝")
	}
	first.Release()
	if err := <-waiterDone; err != nil {
		t.Fatalf("释放后等待者应取得 lease: %v", err)
	}

	first, err = manager.Acquire(context.Background(), managerScope("plugin-a"), result.Connection)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Acquire(context.Background(), managerScope("plugin-a"), result.Connection); err == nil {
		t.Fatal("同一调用方并发超过上限必须超时")
	} else if code, _ := ErrorDetails(err); code != databasev1.ErrorPoolExhausted {
		t.Fatalf("acquire timeout 错误码=%s", code)
	}
	first.Release()
	snapshot := manager.Snapshot()
	if snapshot.QueueRejected != 1 || snapshot.AcquireTimeouts != 1 || snapshot.AcquireSucceeded != 3 {
		t.Fatalf("acquire 指标错误: %+v", snapshot)
	}
}

func TestPoolManagerForcesDrainAndRetriesCloseFailure(t *testing.T) {
	manager, provider, material := newManagerForTest(t, ManagerPolicy{
		NodeMaxOpen: 10, TenantMaxOpen: 10, ConnectionMaxOpen: 10, MaxGenerations: 2,
		MaxWaitersPerPool: 2, MaxConcurrentPerCaller: 2, DrainTimeout: 20 * time.Millisecond, ClosedHistoryLimit: 2,
	})
	provider.closeFailuresNext = 1
	scope := managerScope("plugin-a")
	result, err := manager.Activate(context.Background(), scope, managerSpec(1, 2), material)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := manager.Acquire(context.Background(), scope, result.Connection)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Retire(context.Background(), scope, result.Connection); err != nil {
		t.Fatal(err)
	}
	waitFor(t, time.Second, func() bool { return manager.Snapshot().CloseFailures == 1 })
	if provider.pool(0).isClosed() {
		t.Fatal("首次 Close 失败不得释放预算")
	}
	if manager.Snapshot().NodeReserved != 2 || manager.Snapshot().ForcedDrains != 1 {
		t.Fatalf("强制排空指标或保守预算错误: %+v", manager.Snapshot())
	}
	lease.Release()
	closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := manager.Close(closeCtx); err != nil {
		t.Fatalf("Close 重试应收敛: %v", err)
	}
	if !provider.pool(0).isClosed() || manager.Snapshot().NodeReserved != 0 {
		t.Fatalf("Close 重试后资源未释放: %+v", manager.Snapshot())
	}
}

func TestPoolManagerBoundsClosedHistoryAcrossConnections(t *testing.T) {
	manager, provider, material := newManagerForTest(t, ManagerPolicy{
		NodeMaxOpen: 10, TenantMaxOpen: 10, ConnectionMaxOpen: 4, MaxGenerations: 2,
		MaxWaitersPerPool: 2, MaxConcurrentPerCaller: 2, DrainTimeout: time.Second, ClosedHistoryLimit: 2,
	})
	scope := managerScope("plugin-a")
	for index, resourceID := range []string{"orders.primary", "billing.primary", "audit.primary"} {
		spec := managerSpec(1, 1)
		spec.Ref.ResourceID = resourceID
		result, err := manager.Activate(context.Background(), scope, spec, material)
		if err != nil {
			t.Fatal(err)
		}
		if err := manager.Retire(context.Background(), scope, result.Connection); err != nil {
			t.Fatal(err)
		}
		waitFor(t, time.Second, provider.pool(index).isClosed)
	}
	if snapshot := manager.Snapshot(); len(snapshot.Generations) != 2 || snapshot.NodeReserved != 0 {
		t.Fatalf("关闭历史必须全局有界且不占预算: %+v", snapshot)
	}
}

func TestPoolManagerDoesNotReactivateStaleRevision(t *testing.T) {
	manager, _, material := newManagerForTest(t, ManagerPolicy{
		NodeMaxOpen: 10, TenantMaxOpen: 10, ConnectionMaxOpen: 10, MaxGenerations: 2,
		MaxWaitersPerPool: 2, MaxConcurrentPerCaller: 2, DrainTimeout: time.Second, ClosedHistoryLimit: 2,
	})
	scope := managerScope("plugin-a")
	result, err := manager.Activate(context.Background(), scope, managerSpec(2, 1), material)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Retire(context.Background(), scope, result.Connection); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Activate(context.Background(), scope, managerSpec(1, 1), material); err == nil {
		t.Fatal("退役窗口内不得重新激活更旧的 revision")
	}
}
