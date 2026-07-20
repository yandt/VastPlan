package databaseruntime

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"

	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
)

var dockerContainerName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

type providerFaultFixture struct {
	providerID string
	container  string
	registry   *Registry
	spec       databasev1.ConnectionSpec
	material   *testMaterialSource
	pool       Pool
}

func TestPostgreSQLProviderFaultMatrix(t *testing.T) {
	runProviderFaultMatrix(t, "postgresql", "VASTPLAN_TEST_POSTGRESQL")
}

func TestMySQLProviderFaultMatrix(t *testing.T) {
	runProviderFaultMatrix(t, "mysql", "VASTPLAN_TEST_MYSQL")
}

func runProviderFaultMatrix(t *testing.T, providerID, prefix string) {
	t.Helper()
	fixture := openProviderFaultFixture(t, providerID, prefix)
	defer fixture.pool.Close()

	t.Run("transaction conflict", func(t *testing.T) { fixture.assertTransactionConflict(t) })
	t.Run("pool budget exhaustion", func(t *testing.T) { fixture.assertPoolBudgetExhaustion(t) })
	t.Run("generation forced drain", func(t *testing.T) { fixture.assertGenerationForcedDrain(t) })
	t.Run("network interruption and recovery", func(t *testing.T) { fixture.assertNetworkInterruption(t) })
	t.Run("database restart and recovery", func(t *testing.T) { fixture.assertDatabaseRestart(t) })
}

func openProviderFaultFixture(t *testing.T, providerID, prefix string) *providerFaultFixture {
	t.Helper()
	container := os.Getenv(prefix + "_FAULT_CONTAINER")
	if container == "" {
		t.Skipf("未配置 %s_FAULT_CONTAINER，跳过真实数据库故障矩阵", prefix)
	}
	if !dockerContainerName.MatchString(container) {
		t.Fatalf("%s_FAULT_CONTAINER 不是安全的 Docker 容器名", prefix)
	}
	assertFaultContainerLabel(t, container)
	endpoint, user, password := os.Getenv(prefix+"_ENDPOINT"), os.Getenv(prefix+"_USER"), os.Getenv(prefix+"_PASSWORD")
	if endpoint == "" || user == "" || password == "" {
		t.Fatalf("故障矩阵要求配置 %s_ENDPOINT/USER/PASSWORD", prefix)
	}
	options, err := json.Marshal(map[string]any{
		"user": user, "tlsMode": "disable", "connectTimeoutMs": 1_000,
		"readTimeoutMs": providerTimeout(providerID), "writeTimeoutMs": providerTimeout(providerID),
	})
	if err != nil {
		t.Fatal(err)
	}
	if providerID == "postgresql" {
		options, err = json.Marshal(map[string]any{
			"user": user, "tlsMode": "disable", "connectTimeoutMs": 1_000,
			"applicationName": "vastplan-a5-fault-matrix",
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	spec := providerSpec(providerID, endpoint)
	spec.Database = os.Getenv(prefix + "_DATABASE")
	spec.Options = options
	spec.Pool.MinIdle, spec.Pool.MaxIdle, spec.Pool.MaxOpen = 1, 2, 4
	spec.Pool.AcquireTimeoutMS = 200
	registry := NewRegistry()
	var provider Provider = NewMySQLProvider(ProviderSecurityPolicy{AllowInsecureTLS: true})
	if providerID == "postgresql" {
		provider = NewPostgreSQLProvider(ProviderSecurityPolicy{AllowInsecureTLS: true})
	}
	if err := registry.Register(provider); err != nil {
		t.Fatal(err)
	}
	material := &testMaterialSource{value: []byte(password)}
	pool, err := registry.OpenPool(context.Background(), spec, material)
	if err != nil {
		t.Fatal(err)
	}
	fixture := &providerFaultFixture{
		providerID: providerID, container: container,
		registry: registry, spec: spec, material: material, pool: pool,
	}
	fixture.waitForProbe(t, 30*time.Second)
	return fixture
}

func providerTimeout(providerID string) int {
	if providerID == "mysql" {
		return 1_000
	}
	return 0
}

func (f *providerFaultFixture) assertTransactionConflict(t *testing.T) {
	t.Helper()
	table := "vastplan_a5_deadlock"
	f.execute(t, "drop table if exists "+table)
	if f.providerID == "postgresql" {
		f.execute(t, "create table "+table+" (id bigint primary key, value bigint not null)")
	} else {
		f.execute(t, "create table "+table+" (id bigint primary key, value bigint not null) engine=InnoDB")
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = f.pool.Execute(ctx, databasev1.Statement{SQL: "drop table if exists " + table, Parameters: []databasev1.Value{}})
	})
	f.execute(t, "insert into "+table+" (id, value) values (1, 0)")
	f.execute(t, "insert into "+table+" (id, value) values (2, 0)")

	options := databasev1.TransactionOptions{Isolation: "repeatable-read", TimeoutMS: 10_000}
	tx1, err := f.pool.Begin(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	tx2, err := f.pool.Begin(context.Background(), options)
	if err != nil {
		_ = tx1.Rollback(context.Background())
		t.Fatal(err)
	}
	defer tx1.Rollback(context.Background())
	defer tx2.Rollback(context.Background())
	f.transactionExecute(t, tx1, "update "+table+" set value = value + 1 where id = 1")
	f.transactionExecute(t, tx2, "update "+table+" set value = value + 1 where id = 2")

	start := make(chan struct{})
	errorsSeen := make(chan error, 2)
	for _, operation := range []struct {
		transaction Transaction
		statement   string
	}{{tx1, "update " + table + " set value = value + 1 where id = 2"}, {tx2, "update " + table + " set value = value + 1 where id = 1"}} {
		operation := operation
		go func() {
			<-start
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			_, executeErr := operation.transaction.Execute(ctx, databasev1.Statement{SQL: operation.statement, Parameters: []databasev1.Value{}})
			errorsSeen <- executeErr
		}()
	}
	close(start)
	conflicts := 0
	for range 2 {
		select {
		case operationErr := <-errorsSeen:
			if operationErr == nil {
				continue
			}
			code, retryable := ErrorDetails(operationErr)
			if code == databasev1.ErrorTransactionConflict && retryable {
				conflicts++
				continue
			}
			t.Fatalf("死锁返回了不稳定错误分类: code=%s retryable=%t err=%v", code, retryable, operationErr)
		case <-time.After(10 * time.Second):
			t.Fatal("等待真实数据库死锁检测超时")
		}
	}
	if conflicts == 0 {
		t.Fatal("真实数据库死锁未映射为 retryable database.runtime.transaction_conflict")
	}
}

func (f *providerFaultFixture) assertPoolBudgetExhaustion(t *testing.T) {
	t.Helper()
	policy := ManagerPolicy{
		NodeMaxOpen: 2, TenantMaxOpen: 2, ConnectionMaxOpen: 2, MaxGenerations: 2,
		MaxWaitersPerPool: 1, MaxConcurrentPerCaller: 1, DrainTimeout: time.Second, ClosedHistoryLimit: 2,
	}
	manager, err := NewPoolManager(f.registry, policy)
	if err != nil {
		t.Fatal(err)
	}
	defer closeManager(t, manager)
	spec := f.spec
	spec.Pool.MinIdle, spec.Pool.MaxIdle, spec.Pool.MaxOpen = 0, 1, 1
	spec.Pool.AcquireTimeoutMS = 100
	scope := RequestScope{TenantID: "tenant-a5", ProjectID: "project-a5", CallerID: "cn.vastplan.a5.caller"}
	activated, err := manager.Activate(context.Background(), scope, spec, f.material)
	if err != nil {
		t.Fatal(err)
	}
	materialCalls := f.material.calls
	overBudget := spec
	overBudget.Ref.Revision++
	overBudget.Credentials.Version++
	overBudget.Pool.MaxIdle, overBudget.Pool.MaxOpen = 2, 2
	if _, err := manager.Activate(context.Background(), scope, overBudget, f.material); err == nil {
		t.Fatal("真实 Provider 的 generation 重叠超过连接预算后仍被激活")
	} else if code, retryable := ErrorDetails(err); code != databasev1.ErrorPoolExhausted || !retryable {
		t.Fatalf("generation 预算耗尽错误分类不稳定: code=%s retryable=%t err=%v", code, retryable, err)
	}
	if f.material.calls != materialCalls {
		t.Fatal("generation 预算拒绝必须发生在获取 credential material 之前")
	}
	lease, err := manager.Acquire(context.Background(), scope, activated.Connection)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	if _, err := manager.Acquire(context.Background(), scope, activated.Connection); err == nil {
		t.Fatal("真实 Provider 的调用方并发预算耗尽后仍取得 lease")
	} else if code, retryable := ErrorDetails(err); code != databasev1.ErrorPoolExhausted || !retryable {
		t.Fatalf("预算耗尽错误分类不稳定: code=%s retryable=%t err=%v", code, retryable, err)
	}
	snapshot := manager.Snapshot()
	if snapshot.BudgetRejected != 1 || snapshot.AcquireTimeouts != 1 || snapshot.AcquireSucceeded != 1 {
		t.Fatalf("预算耗尽后计数未收敛: %+v", snapshot)
	}
}

func (f *providerFaultFixture) assertGenerationForcedDrain(t *testing.T) {
	t.Helper()
	policy := ManagerPolicy{
		NodeMaxOpen: 4, TenantMaxOpen: 4, ConnectionMaxOpen: 2, MaxGenerations: 2,
		MaxWaitersPerPool: 2, MaxConcurrentPerCaller: 2, DrainTimeout: 250 * time.Millisecond, ClosedHistoryLimit: 2,
	}
	manager, err := NewPoolManager(f.registry, policy)
	if err != nil {
		t.Fatal(err)
	}
	defer closeManager(t, manager)
	spec := f.spec
	spec.Pool.MinIdle, spec.Pool.MaxIdle, spec.Pool.MaxOpen = 0, 1, 1
	spec.Pool.AcquireTimeoutMS = 200
	scope := RequestScope{TenantID: "tenant-a5", ProjectID: "project-a5", CallerID: "cn.vastplan.a5.transaction"}
	first, err := manager.Activate(context.Background(), scope, spec, f.material)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := manager.Acquire(context.Background(), scope, first.Connection)
	if err != nil {
		t.Fatal(err)
	}
	transactions, err := NewTransactionManager("runtime-a5-"+f.providerID, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer transactions.Close()
	projectID := scope.ProjectID
	call := &contractv1.CallContext{TenantId: scope.TenantID, ProjectId: &projectID, Caller: &contractv1.Caller{
		Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: scope.CallerID,
	}}
	begin, err := transactions.Begin(context.Background(), call, first.Connection,
		databasev1.TransactionOptions{Isolation: "read-committed", TimeoutMS: 5_000}, lease)
	if err != nil {
		lease.Release()
		t.Fatal(err)
	}
	rotated := spec
	rotated.Ref.Revision++
	rotated.Credentials.Version++
	if _, err := manager.Activate(context.Background(), scope, rotated, f.material); err != nil {
		t.Fatal(err)
	}
	select {
	case <-lease.Closed():
	case <-time.After(2 * time.Second):
		t.Fatal("旧 generation 未在 drain 上界后关闭")
	}
	deadline := time.Now().Add(time.Second)
	for {
		_, err = transactions.Query(context.Background(), call, &databasev1.QueryRequest{
			Connection: first.Connection, TransactionHandle: begin.TransactionHandle,
			Statement: databasev1.Statement{SQL: f.scalarQuery(), Parameters: []databasev1.Value{}}, MaxRows: 1,
		})
		if code, retryable := ErrorDetails(err); code == databasev1.ErrorTransactionLost && retryable {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("强制 drain 后事务未稳定收敛为 transaction_lost: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if manager.Snapshot().ForcedDrains != 1 {
		t.Fatalf("强制 drain 计数错误: %+v", manager.Snapshot())
	}
}

func (f *providerFaultFixture) assertNetworkInterruption(t *testing.T) {
	t.Helper()
	f.docker(t, 10*time.Second, "pause", f.container)
	paused := true
	defer func() {
		if paused {
			f.docker(t, 10*time.Second, "unpause", f.container)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
	_, err := f.pool.Query(ctx, databasev1.Statement{SQL: f.scalarQuery(), Parameters: []databasev1.Value{}}, 1)
	cancel()
	if err == nil {
		t.Fatal("数据库网络被冻结时查询不应成功")
	}
	if code, retryable := ErrorDetails(err); code != databasev1.ErrorDeadlineExceeded || !retryable {
		t.Fatalf("网络冻结错误分类不稳定: code=%s retryable=%t err=%v", code, retryable, err)
	}
	f.docker(t, 10*time.Second, "unpause", f.container)
	paused = false
	f.waitForProbe(t, 30*time.Second)
}

func (f *providerFaultFixture) assertDatabaseRestart(t *testing.T) {
	t.Helper()
	f.docker(t, 20*time.Second, "stop", "--time", "0", f.container)
	stopped := true
	defer func() {
		if stopped {
			f.docker(t, 20*time.Second, "start", f.container)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	_, err := f.pool.Query(ctx, databasev1.Statement{SQL: f.scalarQuery(), Parameters: []databasev1.Value{}}, 1)
	cancel()
	if err == nil {
		t.Fatal("数据库停止后查询不应成功")
	}
	code, retryable := ErrorDetails(err)
	if (code != databasev1.ErrorConnectionUnavailable && code != databasev1.ErrorDeadlineExceeded) || !retryable {
		t.Fatalf("数据库停机错误分类不稳定: code=%s retryable=%t err=%v", code, retryable, err)
	}
	f.docker(t, 20*time.Second, "start", f.container)
	stopped = false
	f.waitForProbe(t, 45*time.Second)
}

func (f *providerFaultFixture) waitForProbe(t *testing.T, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last error
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		last = f.pool.Probe(ctx)
		cancel()
		if last == nil {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("数据库未在恢复窗口内重新通过 probe: %v", last)
}

func (f *providerFaultFixture) docker(t *testing.T, timeout time.Duration, arguments ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	command := exec.CommandContext(ctx, "docker", arguments...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("执行 docker %v 失败: %v: %s", arguments, err, output)
	}
}

func (f *providerFaultFixture) scalarQuery() string {
	if f.providerID == "postgresql" {
		return "select 1::bigint"
	}
	return "select cast(1 as signed)"
}

func (f *providerFaultFixture) execute(t *testing.T, statement string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := f.pool.Execute(ctx, databasev1.Statement{SQL: statement, Parameters: []databasev1.Value{}}); err != nil {
		t.Fatal(err)
	}
}

func (f *providerFaultFixture) transactionExecute(t *testing.T, transaction Transaction, statement string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := transaction.Execute(ctx, databasev1.Statement{SQL: statement, Parameters: []databasev1.Value{}}); err != nil {
		t.Fatal(err)
	}
}

func closeManager(t *testing.T, manager *PoolManager) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := manager.Close(ctx); err != nil {
		t.Errorf("关闭故障矩阵 Pool Manager: %v", err)
	}
}

func assertFaultContainerLabel(t *testing.T, container string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "docker", "inspect", "--format",
		`{{ index .Config.Labels "cn.vastplan.test" }}`, container).CombinedOutput()
	if err != nil {
		t.Fatalf("读取故障容器标签失败: %v: %s", err, output)
	}
	if strings.TrimSpace(string(output)) != "a5-database-fault-matrix" {
		t.Fatalf("拒绝操作不属于 A5 夹具的 Docker 容器 %q", container)
	}
}
