package databaseruntime

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
)

func TestPostgreSQLProviderIntegration(t *testing.T) {
	runProviderIntegration(t, "postgresql", "VASTPLAN_TEST_POSTGRESQL", "select $1::bigint as value")
}

func TestMySQLProviderIntegration(t *testing.T) {
	runProviderIntegration(t, "mysql", "VASTPLAN_TEST_MYSQL", "select cast(? as signed) as value")
}

func runProviderIntegration(t *testing.T, providerID, prefix, query string) {
	t.Helper()
	endpoint, user, password := os.Getenv(prefix+"_ENDPOINT"), os.Getenv(prefix+"_USER"), os.Getenv(prefix+"_PASSWORD")
	if endpoint == "" || user == "" || password == "" {
		t.Skipf("未配置 %s_ENDPOINT/USER/PASSWORD，跳过真实数据库集成测试", prefix)
	}
	tlsMode := os.Getenv(prefix + "_TLS_MODE")
	if tlsMode == "" {
		tlsMode = "verify-full"
	}
	options, err := json.Marshal(map[string]any{"user": user, "tlsMode": tlsMode, "serverName": os.Getenv(prefix + "_SERVER_NAME")})
	if err != nil {
		t.Fatal(err)
	}
	spec := providerSpec(providerID, endpoint)
	spec.Database = os.Getenv(prefix + "_DATABASE")
	spec.Options = options
	spec.Pool.MinIdle, spec.Pool.MaxIdle, spec.Pool.MaxOpen = 1, 2, 4
	policy := ProviderSecurityPolicy{AllowInsecureTLS: tlsMode == "disable"}
	registry := NewRegistry()
	var provider Provider
	if providerID == "postgresql" {
		provider = NewPostgreSQLProvider(policy)
	} else {
		provider = NewMySQLProvider(policy)
	}
	if err := registry.Register(provider); err != nil {
		t.Fatal(err)
	}
	pool, err := registry.OpenPool(context.Background(), spec, &testMaterialSource{value: []byte(password)})
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := pool.Probe(ctx); err != nil {
		t.Fatal(err)
	}
	result, err := pool.Query(ctx, databasev1.Statement{SQL: query, Parameters: []databasev1.Value{{
		Type: "int64", Value: json.RawMessage(`"7"`),
	}}}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Rows) != 1 || len(result.Rows[0]) != 1 || result.Rows[0][0].Type != "int64" {
		t.Fatalf("真实 Provider 返回值不符合契约: %+v", result)
	}
}
