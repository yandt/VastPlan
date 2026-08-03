package seedaccess

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	authenticationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authentication/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
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
	if err != nil || len(contributions) != 2 {
		t.Fatalf("Manifest Provider 贡献无效: %+v %v", contributions, err)
	}
	runtimeDescriptors := map[string][]byte{ProviderID: ProviderDescriptor(), HandoffCapability: HandoffDescriptor()}
	for _, contribution := range contributions {
		var signed, runtime any
		if json.Unmarshal(contribution.Descriptor, &signed) != nil || json.Unmarshal(runtimeDescriptors[contribution.ID], &runtime) != nil || !equalJSON(signed, runtime) {
			t.Fatalf("运行态 descriptor 与签名 Manifest 不一致\nsigned=%s\nruntime=%s", contribution.Descriptor, runtimeDescriptors[contribution.ID])
		}
	}
}

func TestSeedProviderUsesGenericAuthenticationMethod(t *testing.T) {
	authority, _ := NewAuthority(FileStore{Path: filepath.Join(t.TempDir(), "seed.json")}, nil)
	authority.now = func() time.Time { return time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC) }
	if _, err := authority.Initialize("owner", []byte("correct horse battery staple")); err != nil {
		t.Fatal(err)
	}
	provider, _ := NewProvider(authority, ProviderOptions{})
	provider.now = authority.now
	contribution := provider.Contribution()
	result, response, err := contribution.Handlers[authenticationv1.OperationDescribe](context.Background(), nil, &contractv1.CallContext{}, []byte(`{}`))
	if err != nil || result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("describe 失败: %+v %v", result, err)
	}
	described, err := authenticationv1.ParseMethodResult(authenticationv1.OperationDescribe, response)
	if err != nil || len(described.(*authenticationv1.DescribeResult).Methods) != 1 {
		t.Fatalf("describe 响应无效: %s %v", response, err)
	}

	begin := authenticationv1.BeginRequest{TransactionID: strings.Repeat("t", 32), MethodID: MethodID, Audience: "portal.example.test", TenantID: "platform", PortalID: "management", Locale: "zh-CN", ClientContextDigest: strings.Repeat("a", 64)}
	beginRaw, _ := json.Marshal(begin)
	result, response, err = contribution.Handlers[authenticationv1.OperationBegin](context.Background(), nil, &contractv1.CallContext{}, beginRaw)
	if err != nil || result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("begin 失败: %+v %v", result, err)
	}
	parsed, err := authenticationv1.ParseMethodResult(authenticationv1.OperationBegin, response)
	if err != nil {
		t.Fatal(err)
	}
	step := parsed.(*authenticationv1.BeginResult).Result.Step
	if len(step.Fields) != 2 {
		t.Fatalf("正常 Seed 登录不应下发恢复租约字段: %+v", step.Fields)
	}

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
	if evidence == nil || evidence.ProviderID != ProviderID || evidence.Subject.ID != SeedOperatorSubjectID {
		t.Fatalf("Seed Provider 未产生标准 Evidence: %+v", evidence)
	}

	result, response, _ = contribution.Handlers[authenticationv1.OperationContinue](context.Background(), nil, &contractv1.CallContext{}, continueRaw)
	parsed, _ = authenticationv1.ParseMethodResult(authenticationv1.OperationContinue, response)
	if result.GetStatus() != contractv1.CallResult_STATUS_OK || parsed.(*authenticationv1.ContinueResult).Result.State != authenticationv1.StateExpired {
		t.Fatal("Provider transaction 必须一次性消费")
	}
}

func TestSeedProviderEnrollsAdministratorOnlyWhileUninitialized(t *testing.T) {
	authority, err := NewAuthority(FileStore{Path: filepath.Join(t.TempDir(), "seed.json")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC)
	authority.now = func() time.Time { return now }
	provider, err := NewProvider(authority, ProviderOptions{AllowInitialSetup: true})
	if err != nil {
		t.Fatal(err)
	}
	provider.now = authority.now
	begin := authenticationv1.BeginRequest{TransactionID: strings.Repeat("i", 32), MethodID: MethodID}
	setup := provider.begin(begin)
	if setup.Result.Step == nil || setup.Result.Step.Kind != authenticationv1.StepEnrollment || len(setup.Result.Step.Fields) != 3 {
		t.Fatalf("未初始化状态必须下发一次性管理员设置表单: %+v", setup)
	}
	if setup.Result.Step.Fields[1].Autocomplete != "new-password" || setup.Result.Step.Fields[2].ID != "password-confirmation" {
		t.Fatalf("管理员设置必须使用 new-password 与确认字段: %+v", setup.Result.Step.Fields)
	}
	completed := provider.continueAuthentication(authenticationv1.ContinueRequest{TransactionID: begin.TransactionID, StepID: setup.Result.Step.StepID, Responses: []authenticationv1.FieldResponse{
		{FieldID: "operator", Value: "admin"}, {FieldID: "password", Value: "a sufficiently long password"}, {FieldID: "password-confirmation", Value: "a sufficiently long password"},
	}})
	if completed.Result.State != authenticationv1.StateAuthenticated || completed.Result.Evidence == nil {
		t.Fatalf("管理员设置成功后必须直接返回认证 Evidence: %+v", completed)
	}
	if err := authority.Authenticate("admin", []byte("a sufficiently long password"), nil); err != nil {
		t.Fatalf("一次性设置必须原子写入 Seed Authority: %v", err)
	}
	if next := provider.begin(authenticationv1.BeginRequest{TransactionID: strings.Repeat("n", 32), MethodID: MethodID}); next.Result.Step == nil || next.Result.Step.Kind == authenticationv1.StepEnrollment {
		t.Fatalf("初始化后不得再次暴露管理员设置入口: %+v", next)
	}
}

func TestSeedProviderRejectsEnrollmentWhenTrustedHostDisablesIt(t *testing.T) {
	authority, _ := NewAuthority(FileStore{Path: filepath.Join(t.TempDir(), "seed.json")}, nil)
	provider, _ := NewProvider(authority, ProviderOptions{})
	result := provider.begin(authenticationv1.BeginRequest{TransactionID: strings.Repeat("d", 32), MethodID: MethodID})
	if result.Result.State != authenticationv1.StateRejected || result.Result.ReasonCode != authenticationv1.ReasonMethodUnavailable {
		t.Fatalf("未获宿主授权时不得开放首次管理员设置: %+v", result)
	}
}

func TestSeedProviderIsReadyForTrustedInitialSetup(t *testing.T) {
	authority, _ := NewAuthority(FileStore{Path: filepath.Join(t.TempDir(), "seed.json")}, nil)
	provider, _ := NewProvider(authority, ProviderOptions{AllowInitialSetup: true})
	result, raw, err := provider.handle(context.Background(), authenticationv1.OperationHealth, []byte(`{}`))
	if err != nil || result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("可信首次设置阶段的健康探针失败: result=%+v err=%v", result, err)
	}
	parsed, err := authenticationv1.ParseMethodResult(authenticationv1.OperationHealth, raw)
	if err != nil || !parsed.(*authenticationv1.HealthResult).Ready {
		t.Fatalf("未初始化且宿主已授权时 Seed Provider 必须保持 Ready: result=%s err=%v", raw, err)
	}
	provider, _ = NewProvider(authority, ProviderOptions{})
	result, raw, err = provider.handle(context.Background(), authenticationv1.OperationHealth, []byte(`{}`))
	if err != nil || result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("禁用首次设置阶段的健康探针失败: result=%+v err=%v", result, err)
	}
	parsed, err = authenticationv1.ParseMethodResult(authenticationv1.OperationHealth, raw)
	if err != nil || parsed.(*authenticationv1.HealthResult).Ready {
		t.Fatalf("未获宿主授权时未初始化 Seed Provider 不得 Ready: result=%s err=%v", raw, err)
	}
}

func TestSeedProviderExposesRecoveryTokenOnlyForActiveLease(t *testing.T) {
	store := FileStore{Path: filepath.Join(t.TempDir(), "seed.json")}
	authority, err := NewAuthority(store, localProof{valid: "console-proof"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	authority.now = func() time.Time { return now }
	state, err := authority.Initialize("owner", []byte("correct horse battery staple"))
	if err != nil {
		t.Fatal(err)
	}
	profile := testRef("corporate-oidc", "a")
	state, err = authority.ConfigureProvider(state.Generation, profile)
	if err != nil {
		t.Fatal(err)
	}
	subject := authenticationv1.SubjectIdentity{ID: "owner", Issuer: "https://identity.example.test"}
	state, err = authority.VerifyProvider(state.Generation, profile, subject)
	if err != nil {
		t.Fatal(err)
	}
	state, err = authority.PrepareHandoff(state.Generation, HandoffSeal{ProviderProfile: profile, Subject: subject, PolicySnapshot: testRef("root-policy", "b"), SessionID: "session.1", AuthenticatedAt: now, ExpiresAt: now.Add(time.Minute), RecoveryReady: true})
	if err != nil {
		t.Fatal(err)
	}
	state, err = authority.CompleteHandoff(state.Generation, state.Handoff.Digest)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := authority.OpenRecovery(state.Generation, []byte("console-proof"), time.Minute); err != nil {
		t.Fatal(err)
	}
	provider, err := NewProvider(authority, ProviderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	provider.now = authority.now
	result := provider.begin(authenticationv1.BeginRequest{TransactionID: strings.Repeat("r", 32), MethodID: MethodID})
	if result.Result.Step == nil || len(result.Result.Step.Fields) != 3 || result.Result.Step.Fields[2].ID != "recovery-token" || !result.Result.Step.Fields[2].Required {
		t.Fatalf("有效恢复租约时必须下发必填恢复字段: %+v", result)
	}
}

func equalJSON(left, right any) bool {
	leftRaw, _ := json.Marshal(left)
	rightRaw, _ := json.Marshal(right)
	return string(leftRaw) == string(rightRaw)
}
