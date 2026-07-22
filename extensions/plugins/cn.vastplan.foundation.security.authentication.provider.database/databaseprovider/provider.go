package databaseprovider

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"sync"
	"time"

	authenticationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authentication/v1"
	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

const PluginID = "cn.vastplan.foundation.security.authentication.provider.database"
const PluginVersion = "0.1.0"
const ProviderID = "database-user"
const MethodID = "database-password"

type transaction struct {
	profileID, stepID string
	expires           time.Time
}
type Provider struct {
	profiles     map[string]Profile
	dummyHash    string
	mu           sync.Mutex
	transactions map[string]transaction
	now          func() time.Time
}

func New(configuration Configuration) (*Provider, error) {
	if err := configuration.Validate(); err != nil {
		return nil, err
	}
	dummy, err := EncodeArgon2id([]byte("vastplan-dummy-password"), []byte("vastplan-dummy-0"))
	if err != nil {
		return nil, err
	}
	return &Provider{profiles: configuration.Profiles, dummyHash: dummy, transactions: map[string]transaction{}, now: func() time.Time { return time.Now().UTC() }}, nil
}
func Descriptor() []byte {
	return []byte(`{"title":"数据库用户认证 Provider","protocol":"authentication.method.v1","purposes":["portal-login"],"methods":[{"id":"database-password","kind":"password","interaction":"form"}],"subjectNamespace":"enterprise.identity.database","requiredCapabilities":["foundation.data.relational.runtime"]}`)
}
func (p *Provider) Contribution() sdk.Contribution {
	handlers := map[string]sdk.Handler{}
	for _, operation := range authenticationv1.ProtocolOperations() {
		op := operation
		handlers[op] = func(ctx context.Context, host sdk.Host, call *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
			return p.handle(ctx, host, call, op, payload)
		}
	}
	return sdk.Contribution{ExtensionPoint: extpoint.AuthenticationProvider, ID: ProviderID, Descriptor: Descriptor(), Handlers: handlers}
}

func (p *Provider) handle(ctx context.Context, host sdk.Host, call *contractv1.CallContext, operation string, payload []byte) (*contractv1.CallResult, []byte, error) {
	request, err := authenticationv1.ParseMethodRequest(operation, payload)
	if err != nil {
		return providerError(err), nil, nil
	}
	var result any
	switch value := request.(type) {
	case *authenticationv1.DescribeRequest:
		result = authenticationv1.DescribeResult{Protocol: authenticationv1.Protocol, Methods: []authenticationv1.MethodDescriptor{{MethodID: MethodID, ProviderID: ProviderID, Kind: authenticationv1.MethodPassword, Interaction: authenticationv1.InteractionForm, DisplayName: authenticationv1.LocalizedText{"zh-CN": "企业账号密码", "en-US": "Enterprise username and password"}, AMR: []string{"pwd"}, ACR: "aal1"}}}
	case *authenticationv1.BeginRequest:
		result = p.begin(*value)
	case *authenticationv1.ContinueRequest:
		result = p.continueLogin(ctx, host, call, *value)
	case *authenticationv1.CancelRequest:
		p.mu.Lock()
		delete(p.transactions, value.TransactionID)
		p.mu.Unlock()
		result = authenticationv1.CancelResult{Cancelled: true}
	case *authenticationv1.ResendRequest:
		result = authenticationv1.ResendResult{Result: authenticationv1.MethodResult{State: authenticationv1.StateRejected, ReasonCode: authenticationv1.ReasonMethodUnavailable}}
	case *authenticationv1.HealthRequest:
		result = authenticationv1.HealthResult{Ready: len(p.profiles) > 0, ProviderID: ProviderID}
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
	profile, ok := p.profiles[request.ProviderProfileID]
	if !ok || request.MethodID != MethodID {
		return authenticationv1.BeginResult{Result: rejected(authenticationv1.ReasonMethodUnavailable)}
	}
	_ = profile
	stepID, _ := randomID()
	expires := p.now().Add(5 * time.Minute)
	p.mu.Lock()
	if len(p.transactions) >= 4096 {
		p.mu.Unlock()
		return authenticationv1.BeginResult{Result: rejected(authenticationv1.ReasonRateLimited)}
	}
	p.transactions[request.TransactionID] = transaction{profileID: request.ProviderProfileID, stepID: stepID, expires: expires}
	p.mu.Unlock()
	return authenticationv1.BeginResult{Result: authenticationv1.MethodResult{State: authenticationv1.StateChallenge, Step: &authenticationv1.AuthenticationStep{StepID: stepID, Kind: authenticationv1.StepPassword, Title: authenticationv1.LocalizedText{"zh-CN": "企业账号登录", "en-US": "Enterprise sign in"}, Description: authenticationv1.LocalizedText{"zh-CN": "输入企业账号和密码", "en-US": "Enter your enterprise credentials"}, SubmitLabel: authenticationv1.LocalizedText{"zh-CN": "登录", "en-US": "Sign in"}, ExpiresAt: expires, Fields: []authenticationv1.AuthenticationField{{ID: "identifier", Kind: authenticationv1.FieldIdentifier, Label: authenticationv1.LocalizedText{"zh-CN": "账号", "en-US": "Account"}, Help: authenticationv1.LocalizedText{"zh-CN": "企业账号标识", "en-US": "Enterprise account identifier"}, Autocomplete: "username", Required: true, MinLength: 1, MaxLength: 320, Choices: []authenticationv1.FieldChoice{}}, {ID: "password", Kind: authenticationv1.FieldPassword, Label: authenticationv1.LocalizedText{"zh-CN": "密码", "en-US": "Password"}, Help: authenticationv1.LocalizedText{"zh-CN": "密码不会写入日志", "en-US": "The password is never logged"}, Autocomplete: "current-password", Required: true, MinLength: 1, MaxLength: 1024, Choices: []authenticationv1.FieldChoice{}}}}}}
}

func (p *Provider) continueLogin(ctx context.Context, host sdk.Host, call *contractv1.CallContext, request authenticationv1.ContinueRequest) authenticationv1.ContinueResult {
	p.mu.Lock()
	transaction, ok := p.transactions[request.TransactionID]
	delete(p.transactions, request.TransactionID)
	p.mu.Unlock()
	if !ok || transaction.stepID != request.StepID || !p.now().Before(transaction.expires) {
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
	profile := p.profiles[transaction.profileID]
	row, err := p.lookup(ctx, host, call, profile, string(fields["identifier"]))
	hash := row.PasswordHash
	if err != nil || hash == "" {
		hash = p.dummyHash
	}
	valid := VerifyArgon2id(hash, fields["password"])
	if err != nil || row.Disabled || !valid {
		return authenticationv1.ContinueResult{Result: rejected(authenticationv1.ReasonInvalidCredentials)}
	}
	now := p.now()
	evidenceID, _ := randomID()
	nonce, _ := randomID()
	return authenticationv1.ContinueResult{Result: authenticationv1.MethodResult{State: authenticationv1.StateAuthenticated, Evidence: &authenticationv1.AuthenticationEvidence{EvidenceID: "database." + evidenceID, TransactionID: request.TransactionID, MethodID: MethodID, ProviderID: ProviderID, Subject: authenticationv1.SubjectIdentity{ID: row.Subject, Issuer: profile.Issuer}, AMR: []string{"pwd"}, ACR: "aal1", AuthenticatedAt: now, ExpiresAt: now.Add(30 * time.Second), Nonce: nonce}}}
}

type userRow struct {
	Subject, PasswordHash string
	Disabled              bool
}

func (p *Provider) lookup(ctx context.Context, host sdk.Host, call *contractv1.CallContext, profile Profile, identifier string) (userRow, error) {
	parameter, _ := json.Marshal(identifier)
	request := databasev1.QueryRequest{Connection: profile.Connection, Statement: databasev1.Statement{SQL: profile.LookupSQL, Parameters: []databasev1.Value{{Type: "string", Value: parameter}}}, MaxRows: 2}
	payload, _ := json.Marshal(request)
	operation := databasev1.OperationQuery
	result, raw, err := host.Call(ctx, &contractv1.CallTarget{ExtensionPoint: extpoint.ToolPackage, Capability: databasev1.Capability, Operation: &operation}, call, payload)
	if err != nil || result == nil || result.Status != contractv1.CallResult_STATUS_OK {
		return userRow{}, errors.New("数据库身份查询不可用")
	}
	var query databasev1.QueryResult
	if err := json.Unmarshal(raw, &query); err != nil || len(query.Rows) != 1 {
		return userRow{}, errors.New("数据库身份不存在或重复")
	}
	values := map[string]databasev1.Value{}
	for index, column := range query.Columns {
		if index < len(query.Rows[0]) {
			values[column.Name] = query.Rows[0][index]
		}
	}
	var row userRow
	if json.Unmarshal(values[profile.SubjectColumn].Value, &row.Subject) != nil || json.Unmarshal(values[profile.PasswordHashColumn].Value, &row.PasswordHash) != nil || json.Unmarshal(values[profile.DisabledColumn].Value, &row.Disabled) != nil || row.Subject == "" || row.PasswordHash == "" {
		return userRow{}, errors.New("数据库身份结果无效")
	}
	return row, nil
}
func rejected(reason string) authenticationv1.MethodResult {
	return authenticationv1.MethodResult{State: authenticationv1.StateRejected, ReasonCode: reason}
}
func randomID() (string, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
func providerError(err error) *contractv1.CallResult {
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: "foundation.authentication.database.invalid_request", Message: err.Error()}}
}
