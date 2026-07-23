// Package configurationscoped is the Go consumer SDK for
// configuration.scoped.v1. The wire contract remains language neutral.
package configurationscoped

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	configurationscopedv1 "cdsoft.com.cn/VastPlan/contracts/schemas/configurationscoped/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func Resolve(ctx context.Context, host sdk.Host, call *contractv1.CallContext, output any) (configurationscopedv1.Resolution, error) {
	if host == nil || output == nil {
		return configurationscopedv1.Resolution{}, errors.New("Scoped Configuration client 依赖不完整")
	}
	request := []byte(`{}`)
	operation := configurationscopedv1.OperationResolve
	result, raw, err := host.Call(ctx, target(operation), call, request)
	if err != nil {
		return configurationscopedv1.Resolution{}, err
	}
	if result == nil || result.Status != contractv1.CallResult_STATUS_OK {
		return configurationscopedv1.Resolution{}, resultError(result)
	}
	var response configurationscopedv1.Resolution
	if err := json.Unmarshal(raw, &response); err != nil {
		return configurationscopedv1.Resolution{}, err
	}
	if err := configurationscopedv1.ValidateResolution(response); err != nil {
		return configurationscopedv1.Resolution{}, err
	}
	if err := json.Unmarshal(response.Values, output); err != nil {
		return configurationscopedv1.Resolution{}, fmt.Errorf("解析 Scoped Configuration values: %w", err)
	}
	return response, nil
}

func WatchRevision(ctx context.Context, host sdk.Host, call *contractv1.CallContext, request configurationscopedv1.WatchRevisionRequest) (configurationscopedv1.RevisionObservation, error) {
	if host == nil {
		return configurationscopedv1.RevisionObservation{}, errors.New("Scoped Configuration client 缺少宿主")
	}
	rawRequest, _ := json.Marshal(request)
	operation := configurationscopedv1.OperationWatchRevision
	result, raw, err := host.Call(ctx, target(operation), call, rawRequest)
	if err != nil {
		return configurationscopedv1.RevisionObservation{}, err
	}
	if result == nil || result.Status != contractv1.CallResult_STATUS_OK {
		return configurationscopedv1.RevisionObservation{}, resultError(result)
	}
	var response configurationscopedv1.RevisionObservation
	if err := json.Unmarshal(raw, &response); err != nil {
		return configurationscopedv1.RevisionObservation{}, err
	}
	return response, configurationscopedv1.ValidateRevisionObservation(response)
}

func target(operation string) *contractv1.CallTarget {
	return &contractv1.CallTarget{ExtensionPoint: configurationscopedv1.ExtensionPoint, Capability: configurationscopedv1.Capability, Operation: &operation}
}

func resultError(result *contractv1.CallResult) error {
	if result == nil || result.GetError() == nil {
		return errors.New("Scoped Configuration resolver 返回空错误")
	}
	return fmt.Errorf("%s: %s", result.GetError().GetCode(), result.GetError().GetMessage())
}
