package otpprovider

import (
	"context"
	"encoding/json"
	"errors"

	authenticationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authentication/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func Descriptor() []byte {
	return []byte(`{"title":"企业一次性验证码认证 Provider","protocol":"authentication.method.v1","purposes":["portal-login"],"methods":[{"id":"enterprise-email-code","kind":"one-time-code","interaction":"form"},{"id":"enterprise-sms-code","kind":"one-time-code","interaction":"form"}],"subjectNamespace":"enterprise.identity.otp","requiredCapabilities":["foundation.security.authentication.delivery"]}`)
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
		result = authenticationv1.DescribeResult{Protocol: authenticationv1.Protocol, Methods: []authenticationv1.MethodDescriptor{{MethodID: EmailMethodID, ProviderID: ProviderID, Kind: authenticationv1.MethodOneTimeCode, Interaction: authenticationv1.InteractionForm, DisplayName: text("邮箱验证码", "Email verification code"), AMR: []string{"otp"}, ACR: "aal1", SupportsResend: true}, {MethodID: SMSMethodID, ProviderID: ProviderID, Kind: authenticationv1.MethodOneTimeCode, Interaction: authenticationv1.InteractionForm, DisplayName: text("短信验证码", "SMS verification code"), AMR: []string{"otp"}, ACR: "aal1", SupportsResend: true}}}
	case *authenticationv1.BeginRequest:
		result = p.begin(*value)
	case *authenticationv1.ContinueRequest:
		result = p.continueAuthentication(ctx, host, call, *value)
	case *authenticationv1.ResendRequest:
		result = p.resend(ctx, host, call, *value)
	case *authenticationv1.CancelRequest:
		p.store.Delete(value.TransactionID)
		result = authenticationv1.CancelResult{Cancelled: true}
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
func providerError(err error) *contractv1.CallResult {
	if err == nil {
		err = errors.New("OTP Provider 请求无效")
	}
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: "foundation.authentication.otp.invalid_request", Message: err.Error()}}
}
