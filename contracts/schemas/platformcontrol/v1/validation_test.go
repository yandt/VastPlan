package platformcontrolv1

import (
	"strings"
	"testing"
)

func TestPlatformControlProfileExcludesSecretMaterial(t *testing.T) {
	raw := []byte(`{"schemaVersion":1,"generation":3,"providerId":"postgresql","endpoint":"db.internal:5432","database":"vastplan_control","schema":"platform","tls":{"mode":"verify-full","serverName":"db.internal"},"username":"vastplan_control","secretRef":{"kind":"systemd-credential","name":"vastplan-platform-db-password"},"contractRange":"^1.0.0"}`)
	profile, err := ParseProfile(raw)
	if err != nil || profile.Generation != 3 || profile.SecretRef.Name == "" {
		t.Fatalf("解析控制库 Profile 失败: %+v %v", profile, err)
	}
	if strings.Contains(string(raw), "password\":") {
		t.Fatal("Profile 不得包含密码字段")
	}
}

func TestPlatformControlProfileRejectsDSNAndRelativeSecretFile(t *testing.T) {
	valid := `{"schemaVersion":1,"generation":1,"providerId":"mysql","endpoint":"db.internal:3306","database":"vastplan","schema":"platform","tls":{"mode":"verify-ca"},"username":"vastplan","secretRef":{"kind":"owner-file","path":"/run/vastplan/platform-db-password"},"contractRange":"^1.0.0"}`
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
	profile := Profile{
		SchemaVersion: 1, Generation: 1, ProviderID: "postgresql", Endpoint: "db.internal:5432",
		Database: "vastplan", Schema: "platform", TLS: TLS{Mode: "verify-full", ServerName: "db.internal"},
		Username: "vastplan", ContractRange: "^1.0.0",
	}
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
