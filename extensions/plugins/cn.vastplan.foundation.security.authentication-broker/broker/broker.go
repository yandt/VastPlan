package broker

import (
	"bytes"
	"context"
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
	PluginID      = "cn.vastplan.foundation.security.authentication-broker"
	PluginVersion = "0.1.0"
	Capability    = "foundation.security.authentication-broker"
)

type Broker struct {
	catalog      Catalog
	transactions TransactionStore
	now          func() time.Time
}

type DescribeRequest struct {
	TenantID string `json:"tenantId"`
	PortalID string `json:"portalId"`
}

func New(catalog Catalog, transactions TransactionStore) (*Broker, error) {
	if catalog == nil || transactions == nil {
		return nil, errors.New("Authentication Broker 需要 Catalog 和 Transaction Store")
	}
	return &Broker{catalog: catalog, transactions: transactions, now: func() time.Time { return time.Now().UTC() }}, nil
}

func Descriptor() []byte {
	return []byte(`{"title":"企业认证 Broker","subcommands":[{"name":"describe","description":"列出门户允许的认证方式"},{"name":"begin","description":"开始认证事务"},{"name":"continue","description":"继续认证事务"},{"name":"resend","description":"重发认证挑战"},{"name":"cancel","description":"取消认证事务"},{"name":"health","description":"检查认证 Provider"}]}`)
}

func (b *Broker) Contribution() sdk.Contribution {
	handlers := map[string]sdk.Handler{}
	for _, operation := range authenticationv1.ProtocolOperations() {
		op := operation
		handlers[op] = func(ctx context.Context, host sdk.Host, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
			return b.Handle(ctx, host, callCtx, op, payload)
		}
	}
	return sdk.Contribution{ExtensionPoint: extpoint.ToolPackage, ID: Capability, Descriptor: Descriptor(), Handlers: handlers}
}

func (b *Broker) Handle(ctx context.Context, host sdk.Host, callCtx *contractv1.CallContext, operation string, payload []byte) (*contractv1.CallResult, []byte, error) {
	if host == nil {
		return brokerError("foundation.authentication.host_unavailable", errors.New("可信宿主不可用")), nil, nil
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
	if err := b.updateTransaction(operation, request, parsed, route, terminal); err != nil {
		return brokerError("foundation.authentication.transaction_unavailable", err), nil, nil
	}
	return result, response, nil
}

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
		return TransactionRoute{ProviderID: provider.ContributionID, ProfileID: provider.Profile.ID, MethodID: begin.MethodID}, false, nil
	}
	transactionID := transactionID(request)
	route, found := b.transactions.Get(transactionID)
	if !found {
		return TransactionRoute{}, true, errors.New("认证事务不存在或已过期")
	}
	return route, operation == authenticationv1.OperationCancel, nil
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
