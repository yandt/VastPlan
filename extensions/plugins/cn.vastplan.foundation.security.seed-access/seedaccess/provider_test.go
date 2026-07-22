package seedaccess

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	authenticationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authentication/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
)

func TestProviderDescriptorMatchesManifest(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "vastplan.plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := pluginv1.ParseManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	contributions, err := pluginv1.BackendRuntimeContributions(manifest)
	if err != nil || len(contributions) != 1 {
		t.Fatalf("Manifest Provider 贡献无效: %+v %v", contributions, err)
	}
	var signed, runtime any
	if json.Unmarshal(contributions[0].Descriptor, &signed) != nil || json.Unmarshal(ProviderDescriptor(), &runtime) != nil || !equalJSON(signed, runtime) {
		t.Fatalf("运行态 descriptor 与签名 Manifest 不一致\nsigned=%s\nruntime=%s", contributions[0].Descriptor, ProviderDescriptor())
	}
}

func TestSeedProviderUsesGenericAuthenticationMethod(t *testing.T) {
	authority, _ := NewAuthority(FileStore{Path: filepath.Join(t.TempDir(), "seed.json")}, nil)
	authority.now = func() time.Time { return time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC) }
	if _, err := authority.Initialize("owner", []byte("correct horse battery staple")); err != nil {
		t.Fatal(err)
	}
	provider, _ := NewProvider(authority)
	provider.now = authority.now
	contribution := provider.Contribution()

	begin := authenticationv1.BeginRequest{TransactionID: strings.Repeat("t", 32), MethodID: MethodID, Audience: "portal.example.test", TenantID: "platform", PortalID: "management", Locale: "zh-CN", ClientContextDigest: strings.Repeat("a", 64)}
	beginRaw, _ := json.Marshal(begin)
	result, response, err := contribution.Handlers[authenticationv1.OperationBegin](context.Background(), nil, &contractv1.CallContext{}, beginRaw)
	if err != nil || result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("begin 失败: %+v %v", result, err)
	}
	parsed, err := authenticationv1.ParseMethodResult(authenticationv1.OperationBegin, response)
	if err != nil {
		t.Fatal(err)
	}
	step := parsed.(*authenticationv1.BeginResult).Result.Step

	continuation := authenticationv1.ContinueRequest{TransactionID: begin.TransactionID, StepID: step.StepID, Responses: []authenticationv1.FieldResponse{{FieldID: "operator", Value: "owner"}, {FieldID: "password", Value: "correct horse battery staple"}, {FieldID: "recovery-token", Value: ""}}}
	continueRaw, _ := json.Marshal(continuation)
	result, response, err = contribution.Handlers[authenticationv1.OperationContinue](context.Background(), nil, &contractv1.CallContext{}, continueRaw)
	if err != nil || result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("continue 失败: %+v %v", result, err)
	}
	parsed, err = authenticationv1.ParseMethodResult(authenticationv1.OperationContinue, response)
	if err != nil {
		t.Fatal(err)
	}
	evidence := parsed.(*authenticationv1.ContinueResult).Result.Evidence
	if evidence == nil || evidence.ProviderID != ProviderID || evidence.Subject.ID != "owner" {
		t.Fatalf("Seed Provider 未产生标准 Evidence: %+v", evidence)
	}

	result, response, _ = contribution.Handlers[authenticationv1.OperationContinue](context.Background(), nil, &contractv1.CallContext{}, continueRaw)
	parsed, _ = authenticationv1.ParseMethodResult(authenticationv1.OperationContinue, response)
	if result.GetStatus() != contractv1.CallResult_STATUS_OK || parsed.(*authenticationv1.ContinueResult).Result.State != authenticationv1.StateExpired {
		t.Fatal("Provider transaction 必须一次性消费")
	}
}

func equalJSON(left, right any) bool {
	leftRaw, _ := json.Marshal(left)
	rightRaw, _ := json.Marshal(right)
	return string(leftRaw) == string(rightRaw)
}
