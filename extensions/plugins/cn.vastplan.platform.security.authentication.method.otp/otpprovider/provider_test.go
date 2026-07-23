package otpprovider

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	authenticationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authentication/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
)

type deliveryHost struct {
	mu       sync.Mutex
	codes    []string
	accepted bool
	fail     bool
}

func (h *deliveryHost) Call(_ context.Context, target *contractv1.CallTarget, _ *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
	if h.fail {
		return nil, nil, errors.New("delivery unavailable")
	}
	if target.GetCapability() != authenticationv1.DeliveryCapability || target.GetOperation() != authenticationv1.OperationDeliver {
		return nil, nil, errors.New("目标无效")
	}
	parsed, err := authenticationv1.ParseDeliveryRequest(authenticationv1.OperationDeliver, payload)
	if err != nil {
		return nil, nil, err
	}
	request := parsed.(*authenticationv1.DeliveryRequest)
	h.mu.Lock()
	h.codes = append(h.codes, request.Code)
	h.mu.Unlock()
	result := authenticationv1.DeliveryResult{Accepted: h.accepted}
	if h.accepted {
		result.SubjectID = "enterprise.alice"
	}
	raw, _ := json.Marshal(result)
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
}
func (h *deliveryHost) latest() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.codes[len(h.codes)-1]
}

func TestOTPProviderResendInvalidatesOldCodeAndConsumesSuccessOnce(t *testing.T) {
	provider, store, clock := testProvider(t)
	host := &deliveryHost{accepted: true}
	transactionID := "t0123456789012345678901234567890"
	begin := callOTP[authenticationv1.BeginResult](t, provider, host, authenticationv1.OperationBegin, beginRequest(transactionID))
	identifierStepID := begin.Result.Step.StepID
	codeStepResult := callOTP[authenticationv1.ContinueResult](t, provider, host, authenticationv1.OperationContinue, authenticationv1.ContinueRequest{TransactionID: transactionID, StepID: identifierStepID, Responses: []authenticationv1.FieldResponse{{FieldID: "identifier", Value: "alice@example.com"}}})
	if codeStepResult.Result.State != authenticationv1.StateChallenge {
		t.Fatalf("未进入验证码步骤: %+v", codeStepResult)
	}
	oldCode := host.latest()
	codeStepID := codeStepResult.Result.Step.StepID
	*clock = clock.Add(6 * time.Second)
	store.now = func() time.Time { return *clock }
	resent := callOTP[authenticationv1.ResendResult](t, provider, host, authenticationv1.OperationResend, authenticationv1.ResendRequest{TransactionID: transactionID, StepID: codeStepID})
	newCode := host.latest()
	if oldCode == newCode || resent.Result.Step.StepID == codeStepID {
		t.Fatal("重发必须生成新 code 和 step")
	}
	wrong := callOTP[authenticationv1.ContinueResult](t, provider, host, authenticationv1.OperationContinue, authenticationv1.ContinueRequest{TransactionID: transactionID, StepID: resent.Result.Step.StepID, Responses: []authenticationv1.FieldResponse{{FieldID: "code", Value: oldCode}}})
	if wrong.Result.State != authenticationv1.StateChallenge {
		t.Fatalf("旧 code 必须失效但允许重试: %+v", wrong)
	}
	authenticated := callOTP[authenticationv1.ContinueResult](t, provider, host, authenticationv1.OperationContinue, authenticationv1.ContinueRequest{TransactionID: transactionID, StepID: resent.Result.Step.StepID, Responses: []authenticationv1.FieldResponse{{FieldID: "code", Value: newCode}}})
	if authenticated.Result.State != authenticationv1.StateAuthenticated || authenticated.Result.Evidence.Subject.ID != "enterprise.alice" {
		t.Fatalf("验证未成功: %+v", authenticated)
	}
	replay := callOTP[authenticationv1.ContinueResult](t, provider, host, authenticationv1.OperationContinue, authenticationv1.ContinueRequest{TransactionID: transactionID, StepID: resent.Result.Step.StepID, Responses: []authenticationv1.FieldResponse{{FieldID: "code", Value: newCode}}})
	if replay.Result.State != authenticationv1.StateExpired {
		t.Fatalf("code 不得重放: %+v", replay)
	}
}

func TestOTPProviderHidesUnknownIdentityAndFailsClosedWithoutDelivery(t *testing.T) {
	provider, _, _ := testProvider(t)
	unknown := &deliveryHost{accepted: false}
	transactionID := "u0123456789012345678901234567890"
	begin := callOTP[authenticationv1.BeginResult](t, provider, unknown, authenticationv1.OperationBegin, beginRequest(transactionID))
	challenge := callOTP[authenticationv1.ContinueResult](t, provider, unknown, authenticationv1.OperationContinue, authenticationv1.ContinueRequest{TransactionID: transactionID, StepID: begin.Result.Step.StepID, Responses: []authenticationv1.FieldResponse{{FieldID: "identifier", Value: "missing@example.com"}}})
	verified := callOTP[authenticationv1.ContinueResult](t, provider, unknown, authenticationv1.OperationContinue, authenticationv1.ContinueRequest{TransactionID: transactionID, StepID: challenge.Result.Step.StepID, Responses: []authenticationv1.FieldResponse{{FieldID: "code", Value: unknown.latest()}}})
	if verified.Result.State != authenticationv1.StateChallenge {
		t.Fatalf("未知主体即使猜中 code 也不得认证: %+v", verified)
	}

	failing := &deliveryHost{fail: true}
	transactionID = "f0123456789012345678901234567890"
	begin = callOTP[authenticationv1.BeginResult](t, provider, failing, authenticationv1.OperationBegin, beginRequest(transactionID))
	failed := callOTP[authenticationv1.ContinueResult](t, provider, failing, authenticationv1.OperationContinue, authenticationv1.ContinueRequest{TransactionID: transactionID, StepID: begin.Result.Step.StepID, Responses: []authenticationv1.FieldResponse{{FieldID: "identifier", Value: "alice@example.com"}}})
	if failed.Result.State != authenticationv1.StateRejected || failed.Result.ReasonCode != authenticationv1.ReasonMethodUnavailable {
		t.Fatalf("Delivery 故障必须 fail closed: %+v", failed)
	}
}

func TestOTPProviderAllowsOnlyOneConcurrentSuccess(t *testing.T) {
	provider, _, _ := testProvider(t)
	host := &deliveryHost{accepted: true}
	transactionID := "c0123456789012345678901234567890"
	begin := callOTP[authenticationv1.BeginResult](t, provider, host, authenticationv1.OperationBegin, beginRequest(transactionID))
	challenge := callOTP[authenticationv1.ContinueResult](t, provider, host, authenticationv1.OperationContinue, authenticationv1.ContinueRequest{TransactionID: transactionID, StepID: begin.Result.Step.StepID, Responses: []authenticationv1.FieldResponse{{FieldID: "identifier", Value: "alice@example.com"}}})
	request := authenticationv1.ContinueRequest{TransactionID: transactionID, StepID: challenge.Result.Step.StepID, Responses: []authenticationv1.FieldResponse{{FieldID: "code", Value: host.latest()}}}
	states := make(chan authenticationv1.MethodState, 2)
	var wait sync.WaitGroup
	wait.Add(2)
	for range 2 {
		go func() {
			defer wait.Done()
			states <- callOTP[authenticationv1.ContinueResult](t, provider, host, authenticationv1.OperationContinue, request).Result.State
		}()
	}
	wait.Wait()
	close(states)
	successes := 0
	for state := range states {
		if state == authenticationv1.StateAuthenticated {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("并发消费必须只成功一次，实际 %d", successes)
	}
}

func TestOTPProviderExpiresAndLocksBoundedAttempts(t *testing.T) {
	provider, store, clock := testProvider(t)
	host := &deliveryHost{accepted: true}
	transactionID := "e0123456789012345678901234567890"
	begin := callOTP[authenticationv1.BeginResult](t, provider, host, authenticationv1.OperationBegin, beginRequest(transactionID))
	challenge := callOTP[authenticationv1.ContinueResult](t, provider, host, authenticationv1.OperationContinue, authenticationv1.ContinueRequest{TransactionID: transactionID, StepID: begin.Result.Step.StepID, Responses: []authenticationv1.FieldResponse{{FieldID: "identifier", Value: "alice@example.com"}}})
	invalidCode := wrongCode(host.latest())
	for attempt := 1; attempt <= 5; attempt++ {
		result := callOTP[authenticationv1.ContinueResult](t, provider, host, authenticationv1.OperationContinue, authenticationv1.ContinueRequest{TransactionID: transactionID, StepID: challenge.Result.Step.StepID, Responses: []authenticationv1.FieldResponse{{FieldID: "code", Value: invalidCode}}})
		if attempt < 5 && result.Result.State != authenticationv1.StateChallenge {
			t.Fatalf("第 %d 次错误应允许有界重试: %+v", attempt, result)
		}
		if attempt == 5 && result.Result.State != authenticationv1.StateLocked {
			t.Fatalf("达到上限必须锁定: %+v", result)
		}
	}

	transactionID = "x0123456789012345678901234567890"
	begin = callOTP[authenticationv1.BeginResult](t, provider, host, authenticationv1.OperationBegin, beginRequest(transactionID))
	challenge = callOTP[authenticationv1.ContinueResult](t, provider, host, authenticationv1.OperationContinue, authenticationv1.ContinueRequest{TransactionID: transactionID, StepID: begin.Result.Step.StepID, Responses: []authenticationv1.FieldResponse{{FieldID: "identifier", Value: "alice@example.com"}}})
	*clock = clock.Add(301 * time.Second)
	store.now = func() time.Time { return *clock }
	expired := callOTP[authenticationv1.ContinueResult](t, provider, host, authenticationv1.OperationContinue, authenticationv1.ContinueRequest{TransactionID: transactionID, StepID: challenge.Result.Step.StepID, Responses: []authenticationv1.FieldResponse{{FieldID: "code", Value: host.latest()}}})
	if expired.Result.State != authenticationv1.StateExpired {
		t.Fatalf("过期 code 必须拒绝: %+v", expired)
	}
}

func TestOTPManifestMatchesRuntimeDescriptor(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "vastplan.plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := pluginv1.ParseManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	items, err := pluginv1.BackendRuntimeContributions(manifest)
	if err != nil || len(items) != 2 {
		t.Fatalf("OTP Manifest 无效: %+v %v", items, err)
	}
	var signed, runtime any
	for _, item := range items {
		if item.ExtensionPoint == "authentication.provider" {
			_ = json.Unmarshal(item.Descriptor, &signed)
		}
	}
	_ = json.Unmarshal(Descriptor(), &runtime)
	if !reflect.DeepEqual(signed, runtime) {
		t.Fatal("OTP 运行 descriptor 与 Manifest 漂移")
	}
}

func TestOTPProfileCanDisableResend(t *testing.T) {
	zero := 0
	configuration, err := (Configuration{Profiles: map[string]Profile{
		"enterprise-email": {
			MethodID:          EmailMethodID,
			DeliveryProfileID: "enterprise-mail",
			Channel:           authenticationv1.DeliveryEmail,
			Issuer:            "urn:vastplan:enterprise-email",
			MaxResends:        &zero,
		},
	}}).normalized()
	if err != nil {
		t.Fatal(err)
	}
	if got := configuration.Profiles["enterprise-email"].maxResends; got != 0 {
		t.Fatalf("显式 maxResends=0 必须禁用重发，实际 %d", got)
	}
}

func testProvider(t *testing.T) (*Provider, *MemoryChallengeStore, *time.Time) {
	t.Helper()
	maxResends := 2
	store := NewMemoryChallengeStore(32)
	provider, err := New(Configuration{Profiles: map[string]Profile{"enterprise-email": {MethodID: EmailMethodID, DeliveryProfileID: "enterprise-mail", Channel: authenticationv1.DeliveryEmail, Issuer: "urn:vastplan:enterprise-email", CodeLength: 6, TTLSeconds: 300, ResendSeconds: 5, MaxAttempts: 5, MaxResends: &maxResends}}}, store)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	provider.now = func() time.Time { return now }
	store.now = func() time.Time { return now }
	return provider, store, &now
}
func beginRequest(id string) authenticationv1.BeginRequest {
	return authenticationv1.BeginRequest{TransactionID: id, MethodID: EmailMethodID, TenantID: "tenant-a", PortalID: "operations", Audience: "portal", Locale: "zh-CN", ClientContextDigest: "a012345678901234567890123456789012345678901234567890123456789012", ProviderProfileID: "enterprise-email"}
}
func wrongCode(code string) string {
	replacement := byte('0')
	if code[0] == '0' {
		replacement = '1'
	}
	return string(append([]byte{replacement}, []byte(code[1:])...))
}
func callOTP[T any](t *testing.T, provider *Provider, host *deliveryHost, operation string, request any) T {
	t.Helper()
	payload, _ := json.Marshal(request)
	result, raw, err := provider.handle(context.Background(), host, &contractv1.CallContext{}, operation, payload)
	if err != nil || result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("OTP %s 调用失败: %v %+v", operation, err, result)
	}
	var output T
	if err := json.Unmarshal(raw, &output); err != nil {
		t.Fatal(err)
	}
	return output
}
