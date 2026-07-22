// Package broker routes governed authentication flows and signs one-use assertions.
package broker

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"time"

	authenticationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authentication/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

const (
	PluginID                   = "cn.vastplan.foundation.security.authentication-broker"
	PluginVersion              = "0.1.0"
	Capability                 = "foundation.security.authentication.broker"
	OperationConsumeAssertion  = "consumeAssertion"
	OperationBeginProviderTest = "beginProviderTest"
)

type Broker struct {
	catalog      Catalog
	transactions TransactionStore
	signer       AssertionSigner
	verifier     AssertionVerifier
	assertions   AssertionStore
	now          func() time.Time
}

type DescribeRequest struct {
	TenantID string `json:"tenantId"`
	PortalID string `json:"portalId"`
}

func New(catalog Catalog, transactions TransactionStore, signers ...AssertionSigner) (*Broker, error) {
	if catalog == nil || transactions == nil {
		return nil, errors.New("Authentication Broker 需要 Catalog 和 Transaction Store")
	}
	var signer AssertionSigner
	if len(signers) > 0 {
		signer = signers[0]
	}
	service := &Broker{catalog: catalog, transactions: transactions, signer: signer, now: func() time.Time { return time.Now().UTC() }}
	if verifier, ok := signer.(AssertionVerifier); ok {
		service.verifier = verifier
		service.assertions = NewMemoryAssertionStore(4096)
	}
	return service, nil
}

func Descriptor() []byte {
	return []byte(`{"title":"企业认证 Broker","subcommands":[{"name":"describe","description":"列出门户允许的认证方式"},{"name":"begin","description":"开始认证事务"},{"name":"continue","description":"继续认证事务"},{"name":"resend","description":"重发认证挑战"},{"name":"cancel","description":"取消认证事务"},{"name":"health","description":"检查认证 Provider"},{"name":"consumeAssertion","description":"原子消费 Broker Assertion"},{"name":"beginProviderTest","description":"对已验证草稿发起隔离认证测试"}]}`)
}

func (b *Broker) Contribution() sdk.Contribution {
	handlers := map[string]sdk.Handler{}
	for _, operation := range authenticationv1.ProtocolOperations() {
		op := operation
		handlers[op] = func(ctx context.Context, host sdk.Host, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
			return b.Handle(ctx, host, callCtx, op, payload)
		}
	}
	handlers[OperationConsumeAssertion] = func(ctx context.Context, host sdk.Host, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
		return b.consumeAssertion(callCtx, payload)
	}
	handlers[OperationBeginProviderTest] = func(ctx context.Context, host sdk.Host, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
		return b.beginProviderTest(ctx, host, callCtx, payload)
	}
	return sdk.Contribution{ExtensionPoint: extpoint.ToolPackage, ID: Capability, Descriptor: Descriptor(), Handlers: handlers}
}

func (b *Broker) Handle(ctx context.Context, host sdk.Host, callCtx *contractv1.CallContext, operation string, payload []byte) (*contractv1.CallResult, []byte, error) {
	if host == nil {
		return brokerError("foundation.authentication.host_unavailable", errors.New("可信宿主不可用")), nil, nil
	}
	if operation == OperationConsumeAssertion {
		return b.consumeAssertion(callCtx, payload)
	}
	if operation == OperationBeginProviderTest {
		return b.beginProviderTest(ctx, host, callCtx, payload)
	}
	if operation == authenticationv1.OperationDescribe {
		return b.describe(ctx, host, callCtx, payload)
	}
	request, err := authenticationv1.ParseMethodRequest(operation, payload)
	if err != nil {
		return brokerError("foundation.authentication.invalid_request", err), nil, nil
	}
	if operation == authenticationv1.OperationHealth {
		return b.health(ctx, host, callCtx)
	}
	route, terminal, err := b.route(operation, request)
	if err != nil {
		return brokerError("foundation.authentication.route_unavailable", err), nil, nil
	}
	if begin, ok := request.(*authenticationv1.BeginRequest); ok {
		begin.ProviderProfileID = route.ProfileID
		payload, _ = json.Marshal(begin)
	}
	result, response, err := callProvider(ctx, host, callCtx, route.ProviderID, operation, payload)
	if err != nil {
		return brokerError("foundation.authentication.provider_failed", err), nil, nil
	}
	if result == nil || result.Status != contractv1.CallResult_STATUS_OK {
		return result, response, nil
	}
	parsed, err := authenticationv1.ParseMethodResult(operation, response)
	if err != nil {
		return brokerError("foundation.authentication.provider_invalid", err), nil, nil
	}
	if operation == authenticationv1.OperationContinue {
		response, err = b.finalizeContinue(parsed.(*authenticationv1.ContinueResult), route)
		if err != nil {
			return brokerError("foundation.authentication.assertion_failed", err), nil, nil
		}
	}
	if err := b.updateTransaction(operation, request, parsed, route, terminal); err != nil {
		return brokerError("foundation.authentication.transaction_unavailable", err), nil, nil
	}
	return result, response, nil
}

type TestProfileCatalog interface {
	ResolveTestProfile(profileID, methodID string) (authenticationv1.ProviderCatalogEntry, bool, error)
}

type BeginProviderTestRequest struct {
	TransactionID       string `json:"transactionId"`
	ProviderProfileID   string `json:"providerProfileId"`
	MethodID            string `json:"methodId"`
	TenantID            string `json:"tenantId"`
	PortalID            string `json:"portalId"`
	Locale              string `json:"locale"`
	ClientContextDigest string `json:"clientContextDigest"`
}

func (b *Broker) beginProviderTest(ctx context.Context, host sdk.Host, callCtx *contractv1.CallContext, raw []byte) (*contractv1.CallResult, []byte, error) {
	if callCtx == nil || callCtx.Caller == nil || callCtx.Caller.Kind != contractv1.CallerKind_CALLER_KIND_SYSTEM || callCtx.Scene != "portal.bff" {
		return brokerError("foundation.authentication.test_forbidden", errors.New("只有可信 Portal BFF 可以发起 Provider 测试")), nil, nil
	}
	catalog, ok := b.catalog.(TestProfileCatalog)
	if !ok {
		return brokerError("foundation.authentication.test_unavailable", errors.New("Catalog 不支持未发布 Provider 测试")), nil, nil
	}
	var request BeginProviderTestRequest
	if err := strictJSON(raw, &request); err != nil {
		return brokerError("foundation.authentication.invalid_request", err), nil, nil
	}
	provider, found, err := catalog.ResolveTestProfile(request.ProviderProfileID, request.MethodID)
	if err != nil || !found {
		return brokerError("foundation.authentication.test_profile_unavailable", errors.New("Provider Profile 未处于 Validated 或不支持该 Method")), nil, nil
	}
	begin := authenticationv1.BeginRequest{TransactionID: request.TransactionID, MethodID: request.MethodID, Audience: "authentication-provider-test", TenantID: request.TenantID, PortalID: request.PortalID, Locale: request.Locale, ClientContextDigest: request.ClientContextDigest, ProviderProfileID: provider.Profile.ID}
	result, response, err := callProvider(ctx, host, callCtx, provider.ContributionID, authenticationv1.OperationBegin, mustMarshal(begin))
	if err != nil || result == nil || result.Status != contractv1.CallResult_STATUS_OK {
		return brokerError("foundation.authentication.provider_failed", errors.New("测试 Provider 不可用")), nil, nil
	}
	parsed, err := authenticationv1.ParseMethodResult(authenticationv1.OperationBegin, response)
	if err != nil {
		return brokerError("foundation.authentication.provider_invalid", err), nil, nil
	}
	route := TransactionRoute{ProviderID: provider.ContributionID, ProfileID: provider.Profile.ID, MethodID: request.MethodID, TenantID: request.TenantID, PortalID: request.PortalID, Audience: "authentication-provider-test"}
	if err := b.updateTransaction(authenticationv1.OperationBegin, &begin, parsed, route, false); err != nil {
		return brokerError("foundation.authentication.transaction_unavailable", err), nil, nil
	}
	return result, response, nil
}

func mustMarshal(value any) []byte { raw, _ := json.Marshal(value); return raw }

func (b *Broker) describe(ctx context.Context, host sdk.Host, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
	var request DescribeRequest
	if err := strictJSON(payload, &request); err != nil || request.TenantID == "" || request.PortalID == "" {
		return brokerError("foundation.authentication.invalid_request", errors.New("describe 需要 tenantId 和 portalId")), nil, nil
	}
	catalog, err := b.catalog.Load()
	if err != nil {
		return brokerError("foundation.authentication.catalog_unavailable", err), nil, nil
	}
	providers, ok := allowedProviders(catalog, request.TenantID, request.PortalID)
	if !ok {
		return brokerError("foundation.authentication.binding_not_found", errors.New("门户未绑定认证 Provider")), nil, nil
	}
	byMethod := map[string]authenticationv1.MethodDescriptor{}
	for _, provider := range providers {
		result, raw, callErr := callProvider(ctx, host, callCtx, provider.ContributionID, authenticationv1.OperationDescribe, []byte(`{}`))
		if callErr != nil || result == nil || result.Status != contractv1.CallResult_STATUS_OK {
			continue
		}
		parsed, parseErr := authenticationv1.ParseMethodResult(authenticationv1.OperationDescribe, raw)
		if parseErr != nil {
			continue
		}
		allowed := map[string]struct{}{}
		for _, method := range provider.Methods {
			allowed[method] = struct{}{}
		}
		for _, method := range parsed.(*authenticationv1.DescribeResult).Methods {
			if _, exists := allowed[method.MethodID]; exists && method.ProviderID == provider.ContributionID {
				byMethod[method.MethodID] = method
			}
		}
	}
	methods := make([]authenticationv1.MethodDescriptor, 0, len(byMethod))
	for _, method := range byMethod {
		methods = append(methods, method)
	}
	sort.Slice(methods, func(i, j int) bool { return methods[i].MethodID < methods[j].MethodID })
	raw, _ := json.Marshal(authenticationv1.DescribeResult{Protocol: authenticationv1.Protocol, Methods: methods})
	if _, err := authenticationv1.ParseMethodResult(authenticationv1.OperationDescribe, raw); err != nil {
		return nil, nil, err
	}
	return okResult(), raw, nil
}

func (b *Broker) route(operation string, request any) (TransactionRoute, bool, error) {
	if begin, ok := request.(*authenticationv1.BeginRequest); ok {
		catalog, err := b.catalog.Load()
		if err != nil {
			return TransactionRoute{}, false, err
		}
		provider, found := catalog.Resolve(begin.TenantID, begin.PortalID, begin.MethodID)
		if !found {
			return TransactionRoute{}, false, errors.New("认证方式未绑定到已发布 Provider")
		}
		return TransactionRoute{ProviderID: provider.ContributionID, ProfileID: provider.Profile.ID, MethodID: begin.MethodID, TenantID: begin.TenantID, PortalID: begin.PortalID, Audience: begin.Audience}, false, nil
	}
	transactionID := transactionID(request)
	route, found := b.transactions.Get(transactionID)
	if !found {
		return TransactionRoute{}, true, errors.New("认证事务不存在或已过期")
	}
	return route, operation == authenticationv1.OperationCancel, nil
}

func (b *Broker) finalizeContinue(result *authenticationv1.ContinueResult, route TransactionRoute) ([]byte, error) {
	if result.Result.State != authenticationv1.StateAuthenticated {
		return json.Marshal(authenticationv1.BrokerContinueResult{Result: result.Result})
	}
	evidence := result.Result.Evidence
	if evidence == nil || evidence.TransactionID == "" || evidence.ProviderID != route.ProviderID || evidence.MethodID != route.MethodID {
		return nil, errors.New("Provider Evidence 与 Broker transaction 不一致")
	}
	if b.signer == nil {
		return nil, errors.New("Authentication Broker 未配置 Assertion signer")
	}
	now := b.now()
	if !evidence.ExpiresAt.After(now) {
		return nil, errors.New("Provider Evidence 已过期")
	}
	assertionID, err := randomBrokerID()
	if err != nil {
		return nil, err
	}
	nonce, err := randomBrokerID()
	if err != nil {
		return nil, err
	}
	payload := authenticationv1.AuthenticationAssertion{SchemaVersion: authenticationv1.SchemaVersion, AssertionID: "assertion." + assertionID, TransactionID: evidence.TransactionID, ProviderID: route.ProviderID, ProviderProfileID: route.ProfileID, Subject: evidence.Subject, TenantID: route.TenantID, PortalID: route.PortalID, Audience: route.Audience, AMR: append([]string(nil), evidence.AMR...), ACR: evidence.ACR, IssuedAt: now, ExpiresAt: now.Add(30 * time.Second), Nonce: nonce}
	signed, err := b.signer.Sign(payload)
	if err != nil {
		return nil, err
	}
	if b.assertions == nil {
		return nil, errors.New("Authentication Broker 未配置 Assertion Store")
	}
	if err := b.assertions.Issue(payload); err != nil {
		return nil, err
	}
	return json.Marshal(authenticationv1.BrokerContinueResult{Result: result.Result, Assertion: &signed})
}

func (b *Broker) consumeAssertion(callCtx *contractv1.CallContext, raw []byte) (*contractv1.CallResult, []byte, error) {
	if callCtx == nil || callCtx.Caller == nil || callCtx.Caller.Kind != contractv1.CallerKind_CALLER_KIND_SYSTEM || callCtx.Scene != "portal.bff" {
		return brokerError("foundation.authentication.consume_forbidden", errors.New("只有可信 Portal BFF 可以消费 Assertion")), nil, nil
	}
	var request authenticationv1.ConsumeAssertionRequest
	if err := strictJSON(raw, &request); err != nil {
		return brokerError("foundation.authentication.invalid_request", err), nil, nil
	}
	assertionRaw, _ := json.Marshal(request.Assertion)
	parsed, err := authenticationv1.ParseSignedAssertion(assertionRaw)
	if err != nil {
		return brokerError("foundation.authentication.assertion_invalid", err), nil, nil
	}
	request.Assertion = parsed
	if b.verifier == nil || b.assertions == nil {
		return brokerError("foundation.authentication.assertion_unavailable", errors.New("Assertion verifier/store 未配置")), nil, nil
	}
	if err := b.verifier.Verify(request.Assertion); err != nil {
		return brokerError("foundation.authentication.assertion_invalid", err), nil, nil
	}
	payload := request.Assertion.Payload
	now := b.now()
	if now.Before(payload.IssuedAt.Add(-5*time.Second)) || !now.Before(payload.ExpiresAt) || payload.Audience != request.Audience || payload.TenantID != request.TenantID || payload.PortalID != request.PortalID || payload.TransactionID != request.TransactionID {
		return brokerError("foundation.authentication.assertion_binding_mismatch", errors.New("Assertion 与 Portal transaction 不匹配")), nil, nil
	}
	if !b.assertions.Consume(payload.AssertionID, payload.ExpiresAt) {
		return brokerError("foundation.authentication.assertion_replayed", errors.New("Assertion 未签发、已过期或已消费")), nil, nil
	}
	response, _ := json.Marshal(authenticationv1.ConsumeAssertionResult{Consumed: true})
	return okResult(), response, nil
}

func randomBrokerID() (string, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func (b *Broker) updateTransaction(operation string, request, result any, route TransactionRoute, terminal bool) error {
	id := transactionID(request)
	if begin, ok := request.(*authenticationv1.BeginRequest); ok {
		id = begin.TransactionID
	}
	if terminal {
		b.transactions.Delete(id)
		return nil
	}
	methodResult := resultValue(result)
	if methodResult == nil || methodResult.State != authenticationv1.StateChallenge || methodResult.Step == nil {
		b.transactions.Delete(id)
		return nil
	}
	if methodResult.Step.ExpiresAt.After(b.now().Add(10 * time.Minute)) {
		return errors.New("Provider transaction TTL 超过 Broker 上限")
	}
	route.ExpiresAt = methodResult.Step.ExpiresAt
	return b.transactions.Put(id, route)
}

func (b *Broker) health(ctx context.Context, host sdk.Host, callCtx *contractv1.CallContext) (*contractv1.CallResult, []byte, error) {
	catalog, err := b.catalog.Load()
	if err != nil {
		return brokerError("foundation.authentication.catalog_unavailable", err), nil, nil
	}
	for _, provider := range catalog.Providers {
		result, raw, callErr := callProvider(ctx, host, callCtx, provider.ContributionID, authenticationv1.OperationHealth, []byte(`{}`))
		if callErr != nil || result == nil || result.Status != contractv1.CallResult_STATUS_OK {
			continue
		}
		parsed, parseErr := authenticationv1.ParseMethodResult(authenticationv1.OperationHealth, raw)
		if parseErr == nil && parsed.(*authenticationv1.HealthResult).Ready {
			response, _ := json.Marshal(authenticationv1.HealthResult{Ready: true, ProviderID: Capability})
			return okResult(), response, nil
		}
	}
	response, _ := json.Marshal(authenticationv1.HealthResult{Ready: false, ProviderID: Capability})
	return okResult(), response, nil
}

func callProvider(ctx context.Context, host sdk.Host, callCtx *contractv1.CallContext, providerID, operation string, payload []byte) (*contractv1.CallResult, []byte, error) {
	target := &contractv1.CallTarget{ExtensionPoint: extpoint.AuthenticationProvider, Capability: providerID, Operation: &operation}
	return host.Call(ctx, target, callCtx, payload)
}

func transactionID(request any) string {
	switch value := request.(type) {
	case *authenticationv1.ContinueRequest:
		return value.TransactionID
	case *authenticationv1.ResendRequest:
		return value.TransactionID
	case *authenticationv1.CancelRequest:
		return value.TransactionID
	}
	return ""
}

func resultValue(result any) *authenticationv1.MethodResult {
	switch value := result.(type) {
	case *authenticationv1.BeginResult:
		return &value.Result
	case *authenticationv1.ContinueResult:
		return &value.Result
	case *authenticationv1.ResendResult:
		return &value.Result
	}
	return nil
}

func strictJSON(raw []byte, output any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(output); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("请求只能包含一个 JSON 文档")
	}
	return nil
}

func okResult() *contractv1.CallResult {
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}
}
func brokerError(code string, err error) *contractv1.CallResult {
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: code, Message: fmt.Sprintf("%v", err)}}
}
