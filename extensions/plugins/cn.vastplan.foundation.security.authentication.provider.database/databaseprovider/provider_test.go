package databaseprovider

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	authenticationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authentication/v1"
	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

type databaseHost struct {
	hash    string
	queries int
}

func (h *databaseHost) Call(_ context.Context, target *contractv1.CallTarget, _ *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
	if target.Capability != databasev1.Capability || target.GetOperation() != databasev1.OperationQuery {
		return nil, nil, nil
	}
	h.queries++
	var request databasev1.QueryRequest
	_ = json.Unmarshal(payload, &request)
	if len(request.Statement.Parameters) != 1 || request.MaxRows != 2 {
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR}, nil, nil
	}
	values := func(value any) databasev1.Value {
		raw, _ := json.Marshal(value)
		return databasev1.Value{Type: "string", Value: raw}
	}
	result := databasev1.QueryResult{Columns: []databasev1.Column{{Name: "subject_id"}, {Name: "password_hash"}, {Name: "disabled"}}, Rows: [][]databasev1.Value{{values("user-1"), values(h.hash), func() databasev1.Value {
		raw, _ := json.Marshal(false)
		return databasev1.Value{Type: "boolean", Value: raw}
	}()}}, Truncated: false}
	raw, _ := json.Marshal(result)
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
}

func testProvider(t *testing.T) *Provider {
	t.Helper()
	provider, err := New(Configuration{Profiles: map[string]Profile{"database-users": {Connection: databasev1.ConnectionRef{ResourceID: "identity", Revision: 1}, LookupSQL: "select subject_id,password_hash,disabled from enterprise_users where login = ?", SubjectColumn: "subject_id", PasswordHashColumn: "password_hash", DisabledColumn: "disabled", Issuer: "database://enterprise-users"}}})
	if err != nil {
		t.Fatal(err)
	}
	provider.now = func() time.Time { return time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC) }
	return provider
}

func TestDatabaseProviderUsesGenericRuntimeAndArgon2id(t *testing.T) {
	hash, _ := EncodeArgon2id([]byte("correct horse battery staple"), []byte("0123456789abcdef"))
	host := &databaseHost{hash: hash}
	provider := testProvider(t)
	contribution := provider.Contribution()
	begin := authenticationv1.BeginRequest{TransactionID: strings.Repeat("t", 32), MethodID: MethodID, ProviderProfileID: "database-users", TenantID: "acme", PortalID: "management", Audience: "portal.example", Locale: "zh-CN", ClientContextDigest: strings.Repeat("a", 64)}
	result, raw, _ := contribution.Handlers[authenticationv1.OperationBegin](context.Background(), host, &contractv1.CallContext{}, marshal(t, begin))
	if result.Status != contractv1.CallResult_STATUS_OK {
		t.Fatal("begin 失败")
	}
	parsed, _ := authenticationv1.ParseMethodResult(authenticationv1.OperationBegin, raw)
	step := parsed.(*authenticationv1.BeginResult).Result.Step
	request := authenticationv1.ContinueRequest{TransactionID: begin.TransactionID, StepID: step.StepID, Responses: []authenticationv1.FieldResponse{{FieldID: "identifier", Value: "alice"}, {FieldID: "password", Value: "correct horse battery staple"}}}
	_, raw, _ = contribution.Handlers[authenticationv1.OperationContinue](context.Background(), host, &contractv1.CallContext{TenantId: "acme"}, marshal(t, request))
	parsed, _ = authenticationv1.ParseMethodResult(authenticationv1.OperationContinue, raw)
	evidence := parsed.(*authenticationv1.ContinueResult).Result.Evidence
	if evidence == nil || evidence.Subject.ID != "user-1" || evidence.Subject.Issuer != "database://enterprise-users" || host.queries != 1 {
		t.Fatalf("数据库认证 Evidence 无效: %+v queries=%d", evidence, host.queries)
	}
}

func TestDatabaseProviderRejectsWrongPasswordWithoutLeakingRow(t *testing.T) {
	hash, _ := EncodeArgon2id([]byte("correct horse battery staple"), []byte("0123456789abcdef"))
	host := &databaseHost{hash: hash}
	provider := testProvider(t)
	begin := authenticationv1.BeginRequest{TransactionID: strings.Repeat("x", 32), MethodID: MethodID, ProviderProfileID: "database-users", TenantID: "acme", PortalID: "management", Audience: "portal.example", Locale: "zh-CN", ClientContextDigest: strings.Repeat("b", 64)}
	_, raw, _ := provider.Contribution().Handlers[authenticationv1.OperationBegin](context.Background(), host, &contractv1.CallContext{}, marshal(t, begin))
	parsed, _ := authenticationv1.ParseMethodResult(authenticationv1.OperationBegin, raw)
	step := parsed.(*authenticationv1.BeginResult).Result.Step
	request := authenticationv1.ContinueRequest{TransactionID: begin.TransactionID, StepID: step.StepID, Responses: []authenticationv1.FieldResponse{{FieldID: "identifier", Value: "alice"}, {FieldID: "password", Value: "wrong password value"}}}
	_, raw, _ = provider.Contribution().Handlers[authenticationv1.OperationContinue](context.Background(), host, &contractv1.CallContext{}, marshal(t, request))
	parsed, _ = authenticationv1.ParseMethodResult(authenticationv1.OperationContinue, raw)
	value := parsed.(*authenticationv1.ContinueResult).Result
	if value.State != authenticationv1.StateRejected || value.ReasonCode != authenticationv1.ReasonInvalidCredentials || strings.Contains(string(raw), "user-1") {
		t.Fatalf("错误密码响应泄露: %s", raw)
	}
}

func TestDatabaseProviderManifestAndDialectTemplates(t *testing.T) {
	for _, query := range []string{"select x from users where login = ?", "select x from users where login = $1"} {
		config := Configuration{Profiles: map[string]Profile{"p": {Connection: databasev1.ConnectionRef{ResourceID: "db", Revision: 1}, LookupSQL: query, SubjectColumn: "s", PasswordHashColumn: "p", DisabledColumn: "d", Issuer: "issuer"}}}
		if err := config.Validate(); err != nil {
			t.Fatalf("通用数据库占位符应通过: %s %v", query, err)
		}
	}
	raw, err := os.ReadFile(filepath.Join("..", "vastplan.plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := pluginv1.ParseManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	items, err := pluginv1.BackendRuntimeContributions(manifest)
	if err != nil || len(items) != 1 {
		t.Fatalf("Manifest 无效: %+v %v", items, err)
	}
	var signed, runtime any
	_ = json.Unmarshal(items[0].Descriptor, &signed)
	_ = json.Unmarshal(Descriptor(), &runtime)
	if !reflect.DeepEqual(signed, runtime) {
		t.Fatal("运行 descriptor 与 Manifest 漂移")
	}
}

func marshal(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

var _ sdk.Host = (*databaseHost)(nil)
