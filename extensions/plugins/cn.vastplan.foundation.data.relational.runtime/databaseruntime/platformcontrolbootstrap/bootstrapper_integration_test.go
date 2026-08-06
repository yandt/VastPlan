package platformcontrolbootstrap

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"cdsoft.com.cn/VastPlan/extensions/libraries/go/sharedstate"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.foundation.data.relational.runtime/databaseruntime"
)

func TestPostgreSQLPlatformControlBootstrapIntegration(t *testing.T) {
	runPlatformControlBootstrapIntegration(t, "postgresql", "VASTPLAN_TEST_POSTGRESQL")
}

func TestMySQLPlatformControlBootstrapIntegration(t *testing.T) {
	runPlatformControlBootstrapIntegration(t, "mysql", "VASTPLAN_TEST_MYSQL")
}

func TestPostgreSQLPlatformControlProvisioningIntegration(t *testing.T) {
	runPlatformControlProvisioningIntegration(t, "postgresql", "VASTPLAN_TEST_POSTGRESQL")
}

func TestMySQLPlatformControlProvisioningIntegration(t *testing.T) {
	runPlatformControlProvisioningIntegration(t, "mysql", "VASTPLAN_TEST_MYSQL")
}

func TestPostgreSQLPlatformControlBackupFixture(t *testing.T) {
	runPlatformControlBackupFixture(t, "postgresql", "VASTPLAN_TEST_POSTGRESQL")
}

func TestMySQLPlatformControlBackupFixture(t *testing.T) {
	runPlatformControlBackupFixture(t, "mysql", "VASTPLAN_TEST_MYSQL")
}

func runPlatformControlBootstrapIntegration(t *testing.T, providerID, prefix string) {
	t.Helper()
	endpoint := os.Getenv(prefix + "_ENDPOINT")
	username := os.Getenv(prefix + "_USER")
	password := os.Getenv(prefix + "_PASSWORD")
	database := os.Getenv(prefix + "_DATABASE")
	if endpoint == "" || username == "" || password == "" || database == "" {
		t.Skipf("未配置 %s_ENDPOINT/USER/PASSWORD/DATABASE，跳过 Platform Control 真实集成测试", prefix)
	}

	schema := database
	if providerID == "postgresql" {
		schema = fmt.Sprintf("vastplan_control_%d", time.Now().UnixNano())
	}
	profile := bootstrapProfile(providerID, endpoint, database, schema, username, "disable", "/tmp/vastplan-platform-control-integration.secret")
	secret := bootstrapSecret(password)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	bootstrapper := newIntegrationBootstrapper(t, providerID)
	if err := bootstrapper.Test(ctx, profile, secret); err != nil {
		t.Fatalf("真实 %s Platform Control 候选测试失败: %v", providerID, err)
	}

	// Two independent Runtime replicas may race during an empty cluster start.
	// Database migration locking must make both initializations idempotent.
	candidates := []*Bootstrapper{
		newIntegrationBootstrapper(t, providerID),
		newIntegrationBootstrapper(t, providerID),
	}
	initializationErrors := make(chan error, 2)
	for _, candidate := range candidates {
		go func(candidate *Bootstrapper) {
			store, err := candidate.Initialize(ctx, profile, secret)
			if store != nil {
				_ = store.Close()
			}
			initializationErrors <- err
		}(candidate)
	}
	for range 2 {
		if err := <-initializationErrors; err != nil {
			t.Fatalf("真实 %s Platform Control 并发初始化失败: %v", providerID, err)
		}
	}

	store, err := bootstrapper.Initialize(ctx, profile, secret)
	if err != nil {
		t.Fatalf("真实 %s Platform Control 初始化失败: %v", providerID, err)
	}
	scope := sharedstate.Scope{
		Kind:         sharedstate.ScopeService,
		PluginID:     "cn.vastplan.integration.platform-control",
		RuntimeScope: "seed-primary",
		Namespace:    providerID,
	}
	key := fmt.Sprintf("restart-%d", time.Now().UnixNano())
	created, err := store.Create(ctx, scope, key, []byte(`{"phase":"initialized"}`))
	if err != nil || created.Revision != 1 {
		_ = store.Close()
		t.Fatalf("真实 %s Platform Control 写入失败: entry=%+v err=%v", providerID, created, err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("关闭真实 %s Platform Control 第一代 Store: %v", providerID, err)
	}

	// Recreate the Registry and Bootstrapper to model a complete Runtime restart,
	// rather than reopening through an existing in-memory pool.
	restarted := newIntegrationBootstrapper(t, providerID)
	reopened, err := restarted.Open(ctx, profile, secret)
	if err != nil {
		t.Fatalf("重启后打开真实 %s Platform Control Store 失败: %v", providerID, err)
	}
	defer reopened.Close()
	entry, err := reopened.Get(ctx, scope, key)
	if err != nil || entry.Revision != 1 || string(entry.Value) != `{"phase":"initialized"}` {
		t.Fatalf("真实 %s Platform Control 重启后状态未保留: entry=%+v err=%v", providerID, entry, err)
	}
	if err := reopened.Delete(ctx, scope, key, entry.Revision); err != nil {
		t.Fatalf("清理真实 %s Platform Control 测试状态: %v", providerID, err)
	}
}

func runPlatformControlProvisioningIntegration(t *testing.T, providerID, prefix string) {
	t.Helper()
	if os.Getenv("VASTPLAN_TEST_DATABASE_PROVISIONING") != "1" {
		t.Skip("未显式启用数据库创建集成测试")
	}
	endpoint := os.Getenv(prefix + "_ENDPOINT")
	username := firstNonEmpty(os.Getenv(prefix+"_PROVISION_USER"), os.Getenv(prefix+"_USER"))
	password := firstNonEmpty(os.Getenv(prefix+"_PROVISION_PASSWORD"), os.Getenv(prefix+"_PASSWORD"))
	if endpoint == "" || username == "" || password == "" {
		t.Fatalf("数据库创建矩阵要求配置 %s_ENDPOINT/PROVISION_USER/PROVISION_PASSWORD", prefix)
	}

	target := fmt.Sprintf("vastplan_provision_%s_%d", providerID, time.Now().UnixNano())
	schema := "vastplan_control"
	if providerID == "mysql" {
		schema = target
	}
	profile := bootstrapProfile(providerID, endpoint, target, schema, username, "disable", "/tmp/vastplan-platform-control-provision.secret")
	secret := bootstrapSecret(password)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	bootstrapper := newIntegrationBootstrapper(t, providerID)
	if err := bootstrapper.Test(ctx, profile, secret); err == nil {
		t.Fatalf("真实 %s 创建测试的目标数据库应在 Provision 前不存在", providerID)
	}
	if err := bootstrapper.Provision(ctx, profile, secret); err != nil {
		t.Fatalf("真实 %s Platform Control 创建数据库失败: %v", providerID, err)
	}
	if err := bootstrapper.Provision(ctx, profile, secret); err != nil {
		t.Fatalf("真实 %s Platform Control 重复创建数据库必须幂等: %v", providerID, err)
	}
	if err := bootstrapper.Test(ctx, profile, secret); err != nil {
		t.Fatalf("真实 %s Platform Control 创建后复测失败: %v", providerID, err)
	}
	store, err := bootstrapper.Initialize(ctx, profile, secret)
	if err != nil {
		t.Fatalf("真实 %s Platform Control 创建后初始化失败: %v", providerID, err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("关闭真实 %s Platform Control 创建测试 Store: %v", providerID, err)
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func newIntegrationBootstrapper(t *testing.T, providerID string) *Bootstrapper {
	t.Helper()
	registry := databaseruntime.NewRegistry()
	policy := databaseruntime.ProviderSecurityPolicy{AllowInsecureTLS: true}
	var provider databaseruntime.Provider
	switch providerID {
	case "postgresql":
		provider = databaseruntime.NewPostgreSQLProvider(policy)
	case "mysql":
		provider = databaseruntime.NewMySQLProvider(policy)
	default:
		t.Fatalf("未知真实数据库 Provider: %s", providerID)
	}
	if err := registry.Register(provider); err != nil {
		t.Fatal(err)
	}
	bootstrapper, err := New(registry)
	if err != nil {
		t.Fatal(err)
	}
	return bootstrapper
}

func runPlatformControlBackupFixture(t *testing.T, providerID, prefix string) {
	t.Helper()
	phase := os.Getenv("VASTPLAN_TEST_PLATFORM_CONTROL_BACKUP_PHASE")
	if phase != "seed" && phase != "verify" {
		t.Skip("未进入 Platform Control 真实备份恢复阶段")
	}
	endpoint := os.Getenv(prefix + "_ENDPOINT")
	username := os.Getenv(prefix + "_USER")
	password := os.Getenv(prefix + "_PASSWORD")
	database := os.Getenv(prefix + "_DATABASE")
	if endpoint == "" || username == "" || password == "" || database == "" {
		t.Fatalf("备份恢复矩阵要求配置 %s_ENDPOINT/USER/PASSWORD/DATABASE", prefix)
	}
	schema := database
	if providerID == "postgresql" {
		schema = "vastplan_control_backup"
	}
	profile := bootstrapProfile(providerID, endpoint, database, schema, username, "disable", "/tmp/vastplan-platform-control-backup.secret")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	bootstrapper := newIntegrationBootstrapper(t, providerID)
	scope := sharedstate.Scope{
		Kind: sharedstate.ScopeService, PluginID: "cn.vastplan.integration.platform-control",
		RuntimeScope: "seed-backup", Namespace: "backup",
	}
	const key = "restore-proof"
	const value = `{"backup":"transaction-consistent","revision":1}`

	if phase == "seed" {
		store, err := bootstrapper.Initialize(ctx, profile, bootstrapSecret(password))
		if err != nil {
			t.Fatalf("准备真实 %s Platform Control 备份 fixture: %v", providerID, err)
		}
		defer store.Close()
		if existing, getErr := store.Get(ctx, scope, key); getErr == nil {
			if deleteErr := store.Delete(ctx, scope, key, existing.Revision); deleteErr != nil {
				t.Fatalf("清理旧备份 fixture: %v", deleteErr)
			}
		} else if !errors.Is(getErr, sharedstate.ErrNotFound) {
			t.Fatalf("读取旧备份 fixture: %v", getErr)
		}
		if _, err := store.Create(ctx, scope, key, []byte(value)); err != nil {
			t.Fatalf("写入真实 %s Platform Control 备份 fixture: %v", providerID, err)
		}
		return
	}

	store, err := bootstrapper.Open(ctx, profile, bootstrapSecret(password))
	if err != nil {
		t.Fatalf("恢复后打开真实 %s Platform Control Store: %v", providerID, err)
	}
	defer store.Close()
	entry, err := store.Get(ctx, scope, key)
	if err != nil || entry.Revision != 1 || string(entry.Value) != value {
		t.Fatalf("真实 %s 备份恢复没有保留 Platform Control CAS 状态: entry=%+v err=%v", providerID, entry, err)
	}
}

func TestPlatformControlBootstrapIntegrationDoesNotExposeSecret(t *testing.T) {
	password := "integration-secret-must-not-leak"
	profile := bootstrapProfile("postgresql", "127.0.0.1:1", "vastplan", "vastplan", "vastplan", "disable", "/tmp/vastplan-platform-control-integration.secret")
	bootstrapper := newIntegrationBootstrapper(t, "postgresql")
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	err := bootstrapper.Test(ctx, profile, bootstrapSecret(password))
	if err == nil {
		t.Fatal("不可达数据库候选不应测试成功")
	}
	if strings.Contains(err.Error(), password) || errors.Is(err, context.Canceled) {
		t.Fatalf("Bootstrap 错误泄露 secret 或错误分类异常: %v", err)
	}
}
