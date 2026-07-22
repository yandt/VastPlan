package authenticationv1_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	authenticationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authentication/v1"
)

func marshal(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func localized(value string) authenticationv1.LocalizedText {
	return authenticationv1.LocalizedText{"zh-CN": value, "en-US": value}
}

func passwordStep(now time.Time) authenticationv1.AuthenticationStep {
	return authenticationv1.AuthenticationStep{
		StepID: strings.Repeat("s", 32), Kind: authenticationv1.StepPassword,
		Title: localized("密码登录"), Description: localized("输入当前密码"), SubmitLabel: localized("登录"),
		Fields: []authenticationv1.AuthenticationField{{
			ID: "password", Kind: authenticationv1.FieldPassword, Label: localized("密码"), Help: localized("请输入密码"),
			Autocomplete: "current-password", Required: true, MinLength: 1, MaxLength: 1024, Choices: []authenticationv1.FieldChoice{},
		}},
		ExpiresAt: now.Add(5 * time.Minute),
	}
}

func TestMethodDescribeSupportsPasswordAndOneTimeCodeWithoutFrontendCode(t *testing.T) {
	result := authenticationv1.DescribeResult{Protocol: authenticationv1.Protocol, Methods: []authenticationv1.MethodDescriptor{
		{MethodID: "password", ProviderID: "native-password", Kind: authenticationv1.MethodPassword, Interaction: authenticationv1.InteractionForm, DisplayName: localized("密码登录"), AMR: []string{"pwd"}, ACR: "aal1", SupportsResend: false},
		{MethodID: "one-time-code", ProviderID: "native-otp", Kind: authenticationv1.MethodOneTimeCode, Interaction: authenticationv1.InteractionForm, DisplayName: localized("验证码登录"), AMR: []string{"otp"}, ACR: "aal1", SupportsResend: true},
	}}
	parsed, err := authenticationv1.ParseMethodResult(authenticationv1.OperationDescribe, marshal(t, result))
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.(*authenticationv1.DescribeResult).Methods) != 2 || len(authenticationv1.ProtocolOperations()) != 6 {
		t.Fatalf("认证方法目录异常: %+v", parsed)
	}
	raw := marshal(t, result)
	raw = append(raw[:len(raw)-1], []byte(`,"component":"ArcoLogin"}`)...)
	if _, err := authenticationv1.ParseMethodResult(authenticationv1.OperationDescribe, raw); err == nil {
		t.Fatal("Method Provider 不得注入前端组件")
	}
	result.Methods[0].DisplayName["zh-CN"] = "<b>密码登录</b>"
	if _, err := authenticationv1.ParseMethodResult(authenticationv1.OperationDescribe, marshal(t, result)); err == nil {
		t.Fatal("Method Provider 本地化文案不得携带 HTML")
	}
}

func TestAuthenticationStepEnforcesLoginSpecificSecretSemantics(t *testing.T) {
	now := time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC)
	valid := authenticationv1.BeginResult{Result: authenticationv1.MethodResult{State: authenticationv1.StateChallenge, Step: pointer(passwordStep(now))}}
	if _, err := authenticationv1.ParseMethodResult(authenticationv1.OperationBegin, marshal(t, valid)); err != nil {
		t.Fatal(err)
	}
	valid.Result.Step.Fields[0].Autocomplete = "new-password"
	if _, err := authenticationv1.ParseMethodResult(authenticationv1.OperationBegin, marshal(t, valid)); err == nil {
		t.Fatal("登录密码不得使用注册/重置场景的 new-password autocomplete")
	}
	otp := passwordStep(now)
	otp.Kind = authenticationv1.StepOneTimeCode
	otp.Fields[0] = authenticationv1.AuthenticationField{
		ID: "code", Kind: authenticationv1.FieldOneTimeCode, Label: localized("验证码"), Help: localized("输入验证码"),
		Autocomplete: "one-time-code", Required: true, MinLength: 6, MaxLength: 6, Choices: []authenticationv1.FieldChoice{},
	}
	resend := now.Add(time.Minute)
	otp.ResendAfter = &resend
	if _, err := authenticationv1.ParseMethodResult(authenticationv1.OperationResend, marshal(t, authenticationv1.ResendResult{Result: authenticationv1.MethodResult{State: authenticationv1.StateChallenge, Step: &otp}})); err != nil {
		t.Fatal(err)
	}
}

func TestAuthenticationRedirectRequiresHTTPSOutsideLoopback(t *testing.T) {
	now := time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC)
	step := authenticationv1.AuthenticationStep{
		StepID: strings.Repeat("s", 32), Kind: authenticationv1.StepRedirect,
		Title: localized("SSO"), Description: localized("继续登录"), SubmitLabel: localized("继续"),
		Fields: []authenticationv1.AuthenticationField{}, RedirectURI: "http://identity.example.test/authorize", ExpiresAt: now.Add(time.Minute),
	}
	result := authenticationv1.BeginResult{Result: authenticationv1.MethodResult{State: authenticationv1.StateChallenge, Step: &step}}
	if _, err := authenticationv1.ParseMethodResult(authenticationv1.OperationBegin, marshal(t, result)); err == nil {
		t.Fatal("企业认证 redirect 不得使用非回环 HTTP")
	}
	step.RedirectURI = "http://127.0.0.1:18080/callback"
	if _, err := authenticationv1.ParseMethodResult(authenticationv1.OperationBegin, marshal(t, result)); err != nil {
		t.Fatalf("本地开发回环 redirect 应可用: %v", err)
	}
	step.RedirectURI = "https://identity.example.test/authorize"
	if _, err := authenticationv1.ParseMethodResult(authenticationv1.OperationBegin, marshal(t, result)); err != nil {
		t.Fatalf("HTTPS redirect 应可用: %v", err)
	}
}

func TestAuthenticatedMethodReturnsShortEvidenceNotRolesOrSession(t *testing.T) {
	now := time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC)
	evidence := authenticationv1.AuthenticationEvidence{
		EvidenceID: "evidence.00000001", TransactionID: strings.Repeat("t", 32), MethodID: "password", ProviderID: "native-password",
		Subject: authenticationv1.SubjectIdentity{ID: "alice", Issuer: "https://identity.example.test"}, AMR: []string{"pwd"}, ACR: "aal1",
		AuthenticatedAt: now, ExpiresAt: now.Add(30 * time.Second), Nonce: strings.Repeat("n", 32),
	}
	result := authenticationv1.ContinueResult{Result: authenticationv1.MethodResult{State: authenticationv1.StateAuthenticated, Evidence: &evidence}}
	if _, err := authenticationv1.ParseMethodResult(authenticationv1.OperationContinue, marshal(t, result)); err != nil {
		t.Fatal(err)
	}
	evidence.ExpiresAt = now.Add(61 * time.Second)
	if _, err := authenticationv1.ParseMethodResult(authenticationv1.OperationContinue, marshal(t, authenticationv1.ContinueResult{Result: authenticationv1.MethodResult{State: authenticationv1.StateAuthenticated, Evidence: &evidence}})); err == nil {
		t.Fatal("Method Evidence 不得成为长时 Session")
	}
}

func TestContinueRejectsDuplicateFieldResponsesAndUnknownOperation(t *testing.T) {
	request := authenticationv1.ContinueRequest{TransactionID: strings.Repeat("t", 32), StepID: strings.Repeat("s", 32), Responses: []authenticationv1.FieldResponse{{FieldID: "password", Value: "first"}, {FieldID: "password", Value: "second"}}}
	if _, err := authenticationv1.ParseMethodRequest(authenticationv1.OperationContinue, marshal(t, request)); err == nil {
		t.Fatal("重复字段响应必须拒绝")
	}
	if _, err := authenticationv1.ParseMethodRequest("issueSession", []byte(`{}`)); err == nil {
		t.Fatal("Method Provider 不得拥有签发 Session 操作")
	}
	leaking := authenticationv1.BeginResult{Result: authenticationv1.MethodResult{State: authenticationv1.StateRejected, ReasonCode: "authentication.user_not_found"}}
	if _, err := authenticationv1.ParseMethodResult(authenticationv1.OperationBegin, marshal(t, leaking)); err == nil {
		t.Fatal("Provider 不得用错误码泄露账号是否存在")
	}
}

func pointer[T any](value T) *T { return &value }
