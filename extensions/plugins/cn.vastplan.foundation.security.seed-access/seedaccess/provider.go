package seedaccess

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"sync"
	"time"

	authenticationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authentication/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

const (
	ProviderID = "seed-local"
	MethodID   = "seed-password"
)

type providerTransaction struct {
	stepID    string
	expiresAt time.Time
}

type Provider struct {
	authority *Authority
	now       func() time.Time
	mu        sync.Mutex
	txns      map[string]providerTransaction
}

func NewProvider(authority *Authority) (*Provider, error) {
	if authority == nil {
		return nil, errors.New("Seed Authority 不能为空")
	}
	return &Provider{authority: authority, now: func() time.Time { return time.Now().UTC() }, txns: map[string]providerTransaction{}}, nil
}

func ProviderDescriptor() []byte {
	return []byte(`{"title":"平台种子访问","protocol":"authentication.method.v1","purposes":["portal-login"],"methods":[{"id":"seed-password","kind":"password","interaction":"form"}],"subjectNamespace":"foundation.identity.seed","requiredCapabilities":[]}`)
}

func (p *Provider) Contribution() sdk.Contribution {
	handlers := map[string]sdk.Handler{}
	for _, operation := range authenticationv1.ProtocolOperations() {
		op := operation
		handlers[op] = func(ctx context.Context, _ sdk.Host, _ *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
			return p.handle(ctx, op, payload)
		}
	}
	return sdk.Contribution{ExtensionPoint: extpoint.AuthenticationProvider, ID: ProviderID, Descriptor: ProviderDescriptor(), Handlers: handlers}
}

func (p *Provider) handle(_ context.Context, operation string, payload []byte) (*contractv1.CallResult, []byte, error) {
	request, err := authenticationv1.ParseMethodRequest(operation, payload)
	if err != nil {
		return providerError("foundation.seed.invalid_request", err), nil, nil
	}
	var result any
	switch typed := request.(type) {
	case *authenticationv1.DescribeRequest:
		result = authenticationv1.DescribeResult{Protocol: authenticationv1.Protocol, Methods: []authenticationv1.MethodDescriptor{{MethodID: MethodID, ProviderID: ProviderID, Kind: authenticationv1.MethodPassword, Interaction: authenticationv1.InteractionForm, DisplayName: authenticationv1.LocalizedText{"zh-CN": "平台种子访问", "en-US": "Platform seed access"}, AMR: []string{"pwd"}, ACR: "seed", SupportsResend: false}}}
	case *authenticationv1.BeginRequest:
		result = p.begin(*typed)
	case *authenticationv1.ContinueRequest:
		result = p.continueAuthentication(*typed)
	case *authenticationv1.ResendRequest:
		result = authenticationv1.ResendResult{Result: authenticationv1.MethodResult{State: authenticationv1.StateRejected, ReasonCode: authenticationv1.ReasonMethodUnavailable}}
	case *authenticationv1.CancelRequest:
		p.mu.Lock()
		delete(p.txns, typed.TransactionID)
		p.mu.Unlock()
		result = authenticationv1.CancelResult{Cancelled: true}
	case *authenticationv1.HealthRequest:
		state, loadErr := p.authority.store.Load()
		result = authenticationv1.HealthResult{Ready: loadErr == nil && state.Phase != Uninitialized && state.Phase != EnterpriseActive, ProviderID: ProviderID}
	default:
		return providerError("foundation.seed.invalid_request", errors.New("未知 Seed Provider 请求")), nil, nil
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, nil, err
	}
	if _, err := authenticationv1.ParseMethodResult(operation, raw); err != nil {
		return nil, nil, err
	}
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
}

func (p *Provider) begin(request authenticationv1.BeginRequest) authenticationv1.BeginResult {
	if request.MethodID != MethodID {
		return authenticationv1.BeginResult{Result: authenticationv1.MethodResult{State: authenticationv1.StateRejected, ReasonCode: authenticationv1.ReasonMethodUnavailable}}
	}
	stepID, err := randomID()
	if err != nil {
		return authenticationv1.BeginResult{Result: authenticationv1.MethodResult{State: authenticationv1.StateRejected, ReasonCode: authenticationv1.ReasonMethodUnavailable}}
	}
	expires := p.now().Add(5 * time.Minute)
	p.mu.Lock()
	for id, transaction := range p.txns {
		if !p.now().Before(transaction.expiresAt) {
			delete(p.txns, id)
		}
	}
	if len(p.txns) >= 64 {
		p.mu.Unlock()
		return authenticationv1.BeginResult{Result: authenticationv1.MethodResult{State: authenticationv1.StateRejected, ReasonCode: authenticationv1.ReasonRateLimited}}
	}
	p.txns[request.TransactionID] = providerTransaction{stepID: stepID, expiresAt: expires}
	p.mu.Unlock()
	return authenticationv1.BeginResult{Result: authenticationv1.MethodResult{State: authenticationv1.StateChallenge, Step: &authenticationv1.AuthenticationStep{
		StepID: stepID, Kind: authenticationv1.StepPassword,
		Title:       authenticationv1.LocalizedText{"zh-CN": "平台种子访问", "en-US": "Platform seed access"},
		Description: authenticationv1.LocalizedText{"zh-CN": "仅用于首次配置或本机授权的灾难恢复", "en-US": "Only for initial setup or locally authorized recovery"},
		SubmitLabel: authenticationv1.LocalizedText{"zh-CN": "验证", "en-US": "Verify"}, ExpiresAt: expires,
		Fields: []authenticationv1.AuthenticationField{
			{ID: "operator", Kind: authenticationv1.FieldIdentifier, Label: authenticationv1.LocalizedText{"zh-CN": "种子操作员", "en-US": "Seed operator"}, Help: authenticationv1.LocalizedText{"zh-CN": "首次设置的种子操作员标识", "en-US": "Seed operator configured during setup"}, Autocomplete: "username", Required: true, MinLength: 1, MaxLength: 256, Choices: []authenticationv1.FieldChoice{}},
			{ID: "password", Kind: authenticationv1.FieldPassword, Label: authenticationv1.LocalizedText{"zh-CN": "密码", "en-US": "Password"}, Help: authenticationv1.LocalizedText{"zh-CN": "不会写入日志或会话", "en-US": "Never stored in logs or sessions"}, Autocomplete: "current-password", Required: true, MinLength: 12, MaxLength: 1024, Choices: []authenticationv1.FieldChoice{}},
			{ID: "recovery-token", Kind: authenticationv1.FieldOneTimeCode, Label: authenticationv1.LocalizedText{"zh-CN": "恢复租约", "en-US": "Recovery lease"}, Help: authenticationv1.LocalizedText{"zh-CN": "正常首次配置时留空", "en-US": "Leave empty during initial setup"}, Autocomplete: "one-time-code", Required: false, MinLength: 4, MaxLength: 32, Choices: []authenticationv1.FieldChoice{}},
		},
	}}}
}

func (p *Provider) continueAuthentication(request authenticationv1.ContinueRequest) authenticationv1.ContinueResult {
	p.mu.Lock()
	transaction, exists := p.txns[request.TransactionID]
	if exists {
		delete(p.txns, request.TransactionID)
	}
	p.mu.Unlock()
	if !exists || transaction.stepID != request.StepID || !p.now().Before(transaction.expiresAt) {
		return authenticationv1.ContinueResult{Result: authenticationv1.MethodResult{State: authenticationv1.StateExpired, ReasonCode: authenticationv1.ReasonTransactionInvalid}}
	}
	fields := map[string][]byte{}
	for _, response := range request.Responses {
		fields[response.FieldID] = []byte(response.Value)
	}
	defer func() {
		for _, value := range fields {
			clear(value)
		}
	}()
	if err := p.authority.Authenticate(string(fields["operator"]), fields["password"], fields["recovery-token"]); err != nil {
		return authenticationv1.ContinueResult{Result: authenticationv1.MethodResult{State: authenticationv1.StateRejected, ReasonCode: authenticationv1.ReasonInvalidCredentials}}
	}
	now := p.now()
	evidenceID, _ := randomID()
	nonce, _ := randomID()
	return authenticationv1.ContinueResult{Result: authenticationv1.MethodResult{State: authenticationv1.StateAuthenticated, Evidence: &authenticationv1.AuthenticationEvidence{
		EvidenceID: "seed." + evidenceID, TransactionID: request.TransactionID, MethodID: MethodID, ProviderID: ProviderID,
		Subject: authenticationv1.SubjectIdentity{ID: string(fields["operator"]), Issuer: "vastplan://seed-access"},
		AMR:     []string{"pwd"}, ACR: "seed", AuthenticatedAt: now, ExpiresAt: now.Add(30 * time.Second), Nonce: nonce,
	}}}
}

func randomID() (string, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func providerError(code string, err error) *contractv1.CallResult {
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: code, Message: err.Error()}}
}
