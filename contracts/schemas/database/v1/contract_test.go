package databasev1_test

import (
	"encoding/json"
	"strings"
	"testing"

	commonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/common/v1"
	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
)

func validConnection() databasev1.ConnectionSpec {
	return databasev1.ConnectionSpec{
		Ref:        databasev1.ConnectionRef{ResourceID: "orders.primary", Revision: 3},
		ProviderID: "postgresql", Endpoint: "db.internal:5432", Database: "orders",
		Options: json.RawMessage(`{"tls":{"mode":"verify-full"}}`),
		Credentials: commonv1.ManagedCredentialRef{
			Handle: "credential://managed/orders-primary", Scope: "tenant",
			Owner:   "cn.vastplan.platform.data.relational.connection-manager",
			Purpose: "database.connection", Version: 2,
		},
		Pool: databasev1.PoolPolicy{
			MinIdle: 1, MaxIdle: 4, MaxOpen: 16, MaxLifetimeMS: 3600000,
			MaxIdleTimeMS: 300000, AcquireTimeoutMS: 5000, IdlePoolTTLMS: 600000,
		},
	}
}

func TestDatabaseRuntimeSchemaParsesStrictOperationRequests(t *testing.T) {
	request := databasev1.ActivateRequest{Connection: validConnection()}
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := databasev1.ParseRequest(databasev1.OperationActivate, raw)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.(*databasev1.ActivateRequest).Connection.ProviderID != "postgresql" {
		t.Fatalf("解析结果错误: %+v", parsed)
	}

	withUnknown := raw[:len(raw)-1]
	withUnknown = append(withUnknown, []byte(`,"password":"secret"}`)...)
	if _, err := databasev1.ParseRequest(databasev1.OperationActivate, withUnknown); err == nil {
		t.Fatal("未知字段必须被 Schema 拒绝")
	}
	if _, err := databasev1.ParseRequest("unknown", []byte(`{}`)); err == nil {
		t.Fatal("未知操作必须被拒绝")
	}
}

func TestTransactionRequestsAcceptOpaqueInstanceAffineHandle(t *testing.T) {
	begin, err := json.Marshal(databasev1.BeginRequest{Connection: validConnection().Ref,
		Options: databasev1.TransactionOptions{Isolation: "serializable", ReadOnly: true, TimeoutMS: 1000}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := databasev1.ParseRequest(databasev1.OperationBegin, begin); err != nil {
		t.Fatalf("有效 begin 请求被拒绝: %v", err)
	}
	handle := "vptx1.cnVudGltZS12MQ.ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_abcd"
	end, _ := json.Marshal(databasev1.EndTransactionRequest{TransactionHandle: handle})
	if _, err := databasev1.ParseRequest(databasev1.OperationCommit, end); err != nil {
		t.Fatalf("带路由分段的不透明事务句柄被拒绝: %v", err)
	}
	invalid, _ := json.Marshal(databasev1.EndTransactionRequest{TransactionHandle: "vptx1/runtime/forged"})
	if _, err := databasev1.ParseRequest(databasev1.OperationRollback, invalid); err == nil {
		t.Fatal("含非法字符的事务句柄必须被 Schema 拒绝")
	}
}

func TestConnectionSpecRejectsDSNAndSecretOptions(t *testing.T) {
	spec := validConnection()
	spec.Endpoint = "postgres://user:password@db.internal/orders"
	if err := databasev1.ValidateConnectionSpec(spec); err == nil || !strings.Contains(err.Error(), "DSN") {
		t.Fatalf("含凭证 DSN 必须拒绝: %v", err)
	}
	spec = validConnection()
	spec.Options = json.RawMessage(`{"tls":{"clientPrivateKey":"plaintext"}}`)
	if err := databasev1.ValidateConnectionSpec(spec); err == nil || !strings.Contains(err.Error(), "CredentialRef") {
		t.Fatalf("嵌套秘密选项必须拒绝: %v", err)
	}
	spec = validConnection()
	spec.Pool.MinIdle = spec.Pool.MaxIdle + 1
	if err := databasev1.ValidateConnectionSpec(spec); err == nil {
		t.Fatal("无效池大小关系必须拒绝")
	}
}

func TestConnectionRefValidationUsesWireSchema(t *testing.T) {
	if err := databasev1.ValidateConnectionRef(databasev1.ConnectionRef{ResourceID: "orders.primary", Revision: 1}); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []databasev1.ConnectionRef{
		{ResourceID: "Orders Primary", Revision: 1},
		{ResourceID: "orders.primary", Revision: 0},
	} {
		if err := databasev1.ValidateConnectionRef(invalid); err == nil {
			t.Fatalf("非法 ConnectionRef 必须拒绝: %+v", invalid)
		}
	}
}

func TestTypedValuesRemainLosslessAcrossLanguages(t *testing.T) {
	valid := []databasev1.Value{
		{Type: "null"},
		{Type: "string", Value: json.RawMessage(`"hello"`)},
		{Type: "int64", Value: json.RawMessage(`"9223372036854775807"`)},
		{Type: "decimal", Value: json.RawMessage(`"1234567890.0123456789"`)},
		{Type: "bool", Value: json.RawMessage(`true`)},
		{Type: "bytes", Value: json.RawMessage(`"aGVsbG8="`)},
		{Type: "timestamp", Value: json.RawMessage(`"2026-07-20T12:00:00.123Z"`)},
		{Type: "json", Value: json.RawMessage(`{"nested":true}`)},
	}
	for _, value := range valid {
		if err := databasev1.ValidateValue(value); err != nil {
			t.Fatalf("有效 %s value 被拒绝: %v", value.Type, err)
		}
	}
	for _, invalid := range []databasev1.Value{
		{Type: "int64", Value: json.RawMessage(`9223372036854775807`)},
		{Type: "decimal", Value: json.RawMessage(`"1.2.3"`)},
		{Type: "bytes", Value: json.RawMessage(`"not base64"`)},
		{Type: "timestamp", Value: json.RawMessage(`"today"`)},
	} {
		if databasev1.ValidateValue(invalid) == nil {
			t.Fatalf("非法 %s value 必须拒绝", invalid.Type)
		}
	}
}

func TestProviderAndQueryResultContracts(t *testing.T) {
	descriptor := databasev1.ProviderDescriptor{
		ID: "postgresql", Version: "1.0.0", DisplayName: "PostgreSQL",
		ConfigurationSchema: json.RawMessage(`{"type":"object","additionalProperties":false}`),
		Capabilities:        databasev1.ProviderCapabilities{Query: true, Execute: true, Transactions: true},
	}
	if err := databasev1.ValidateProviderDescriptor(descriptor); err != nil {
		t.Fatal(err)
	}
	descriptor.ID = "psql"
	if err := databasev1.ValidateProviderDescriptor(descriptor); err == nil {
		t.Fatal("psql 不得作为 Provider ID")
	}
	descriptor.ID = "postgresql"
	descriptor.ConfigurationSchema = json.RawMessage(`{"type":"object","properties":{"mode":{"$ref":"https://attacker.invalid/schema.json"}}}`)
	if err := databasev1.ValidateProviderDescriptor(descriptor); err == nil || !strings.Contains(err.Error(), "外部 Schema") {
		t.Fatalf("外部 Schema 引用必须拒绝: %v", err)
	}
	result := databasev1.QueryResult{
		Columns: []databasev1.Column{{Name: "id", DatabaseType: "bigint", Nullable: false}},
		Rows:    [][]databasev1.Value{{{Type: "int64", Value: json.RawMessage(`"42"`)}}},
	}
	if err := databasev1.ValidateQueryResult(result); err != nil {
		t.Fatal(err)
	}
	result.Rows[0] = append(result.Rows[0], databasev1.Value{Type: "null"})
	if err := databasev1.ValidateQueryResult(result); err == nil {
		t.Fatal("行列数量不一致必须拒绝")
	}
}

func TestDatabaseRuntimeErrorCodesAreStable(t *testing.T) {
	for _, code := range []string{
		databasev1.ErrorInvalidRequest, databasev1.ErrorProviderNotFound, databasev1.ErrorUnsupported,
		databasev1.ErrorConnectionNotFound, databasev1.ErrorConnectionUnavailable,
		databasev1.ErrorPoolExhausted, databasev1.ErrorDeadlineExceeded, databasev1.ErrorQueryFailed,
		databasev1.ErrorTransactionLost, databasev1.ErrorTransactionExpired, databasev1.ErrorTransactionConflict,
	} {
		if !databasev1.KnownErrorCode(code) {
			t.Fatalf("稳定错误码未登记: %s", code)
		}
	}
}
