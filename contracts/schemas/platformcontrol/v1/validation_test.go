package platformcontrolv1

import (
	"encoding/json"
	"strings"
	"testing"

	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
)

func TestPlatformControlProfileExcludesSecretMaterial(t *testing.T) {
	raw := []byte(`{"schemaVersion":1,"generation":3,"connection":{"providerId":"postgresql","endpoint":"db.internal:5432","database":"vastplan_control","options":{"user":"vastplan_control","tlsMode":"verify-full","serverName":"db.internal"},"pool":{"maxIdle":8,"maxOpen":32,"maxLifetimeMs":1800000,"maxIdleTimeMs":300000,"acquireTimeoutMs":5000,"idlePoolTtlMs":900000}},"schema":"platform","secretRef":{"kind":"systemd-credential","name":"vastplan-platform-db-password"},"contractRange":"^1.0.0"}`)
	profile, err := ParseProfile(raw)
	if err != nil || profile.Generation != 3 || profile.SecretRef.Name == "" {
		t.Fatalf("解析控制库 Profile 失败: %+v %v", profile, err)
	}
	if strings.Contains(string(raw), "password\":") {
		t.Fatal("Profile 不得包含密码字段")
	}
}

func TestPlatformControlProfileRejectsDSNAndRelativeSecretFile(t *testing.T) {
	valid := `{"schemaVersion":1,"generation":1,"connection":{"providerId":"mysql","endpoint":"db.internal:3306","database":"vastplan","options":{"user":"vastplan","tlsMode":"verify-ca"},"pool":{"maxIdle":8,"maxOpen":32,"maxLifetimeMs":1800000,"maxIdleTimeMs":300000,"acquireTimeoutMs":5000,"idlePoolTtlMs":900000}},"schema":"platform","secretRef":{"kind":"owner-file","path":"/run/vastplan/platform-db-password"},"contractRange":"^1.0.0"}`
	for _, raw := range []string{
		strings.Replace(valid, "db.internal:3306", "mysql://user:secret@db.internal/vastplan", 1),
		strings.Replace(valid, "/run/vastplan/platform-db-password", "relative/password", 1),
	} {
		if _, err := ParseProfile([]byte(raw)); err == nil {
			t.Fatalf("危险 Profile 必须拒绝: %s", raw)
		}
	}
}

func TestChangeRequestAcceptsExactlyOneSecretInput(t *testing.T) {
	profile := testProfile("db.internal:5432")
	if err := ValidateChangeRequest(ChangeRequest{Profile: profile, SecretMaterial: "direct-password"}); err != nil {
		t.Fatalf("一次性密码材料应有效: %v", err)
	}
	profile.SecretRef = SecretRef{Kind: "systemd-credential", Name: "platform-db"}
	if err := ValidateChangeRequest(ChangeRequest{Profile: profile}); err != nil {
		t.Fatalf("外部引用应有效: %v", err)
	}
	if err := ValidateChangeRequest(ChangeRequest{Profile: profile, SecretMaterial: "ambiguous"}); err == nil {
		t.Fatal("密码材料与外部引用不得同时提交")
	}
	profile.SecretRef = SecretRef{}
	if err := ValidateChangeRequest(ChangeRequest{Profile: profile, ExpectedGeneration: 1, SecretMaterial: "stale"}); err == nil {
		t.Fatal("候选 generation 必须紧随 expectedGeneration")
	}
}

func TestChangeRequestReturnsSafeStructuredValidationIssue(t *testing.T) {
	profile := testProfile("missing-port")
	err := ValidateChangeRequest(ChangeRequest{Profile: profile, SecretMaterial: "do-not-report"})
	issue, ok := ValidationIssueFrom(err)
	if !ok || issue.Field != "profile.connection.endpoint" || issue.Reason != "host_port_required" {
		t.Fatalf("应返回稳定字段化校验原因: issue=%+v err=%v", issue, err)
	}
	if strings.Contains(err.Error(), "missing-port") || strings.Contains(err.Error(), "do-not-report") {
		t.Fatal("校验错误不得包含候选字段值或密码")
	}
}

func testProfile(endpoint string) Profile {
	return Profile{
		SchemaVersion: 1, Generation: 1,
		Connection: databasev1.ConnectionCandidate{
			ProviderID: "postgresql", Endpoint: endpoint, Database: "vastplan",
			Options: json.RawMessage(`{"user":"vastplan","tlsMode":"verify-full","serverName":"db.internal"}`), Pool: databasev1.DefaultPoolPolicy(),
		},
		Schema: "platform", ContractRange: "^1.0.0",
	}
}
