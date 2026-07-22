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

func deliver(ctx context.Context, host sdk.Host, call *contractv1.CallContext, request authenticationv1.DeliveryRequest) (authenticationv1.DeliveryResult, error) {
	raw, err := json.Marshal(request)
	if err != nil {
		return authenticationv1.DeliveryResult{}, err
	}
	operation := authenticationv1.OperationDeliver
	result, response, err := host.Call(ctx, &contractv1.CallTarget{ExtensionPoint: extpoint.ToolPackage, Capability: authenticationv1.DeliveryCapability, Operation: &operation}, call, raw)
	if err != nil || result == nil || result.Status != contractv1.CallResult_STATUS_OK {
		return authenticationv1.DeliveryResult{}, errors.New("Authentication Delivery 不可用")
	}
	parsed, err := authenticationv1.ParseDeliveryResult(operation, response)
	if err != nil {
		return authenticationv1.DeliveryResult{}, err
	}
	return *parsed.(*authenticationv1.DeliveryResult), nil
}
