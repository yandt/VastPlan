// Package configurationcontroller adapts a Go plugin-owned hot configuration
// controller to the stable configuration.v1 wire contract. The state machine
// remains owned by the target plugin; this package only enforces caller,
// decoding, response and contribution identity invariants.
package configurationcontroller

import (
	"context"
	"encoding/json"
	"errors"

	configurationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/configuration/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfiguration"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

type Controller interface {
	Prepare(context.Context, sdk.Host, *contractv1.CallContext, configurationv1.PrepareRequest) (configurationv1.Observation, error)
	Commit(context.Context, sdk.Host, *contractv1.CallContext, configurationv1.CandidateRequest) (configurationv1.Observation, error)
	Abort(context.Context, sdk.Host, *contractv1.CallContext, configurationv1.CandidateRequest) (configurationv1.Observation, error)
	Status(context.Context, sdk.Host, *contractv1.CallContext, configurationv1.StatusRequest) (configurationv1.Observation, error)
}

func Contribution(pluginID string, controller Controller) (sdk.Contribution, error) {
	if controller == nil {
		return sdk.Contribution{}, errors.New("configuration.v1 controller 不能为空")
	}
	capability, err := pluginv1.ConfigurationControllerCapability(pluginID)
	if err != nil {
		return sdk.Contribution{}, err
	}
	handler := func(operation string) sdk.Handler {
		return func(ctx context.Context, host sdk.Host, call *contractv1.CallContext, raw []byte) (*contractv1.CallResult, []byte, error) {
			if call.GetTenantId() == "" || call.GetCaller().GetKind() != contractv1.CallerKind_CALLER_KIND_PLUGIN || call.GetCaller().GetId() != pluginconfiguration.PluginSettingsID {
				return controllerError("configuration.controller.permission_denied", errors.New("configuration.v1 只接受 plugin-settings 认证调用"))
			}
			parsed, err := configurationv1.ParseRequest(operation, raw)
			if err != nil {
				return controllerError("configuration.controller.invalid_request", err)
			}
			var observation configurationv1.Observation
			switch request := parsed.(type) {
			case *configurationv1.PrepareRequest:
				observation, err = controller.Prepare(ctx, host, call, *request)
			case *configurationv1.CandidateRequest:
				if operation == configurationv1.OperationCommit {
					observation, err = controller.Commit(ctx, host, call, *request)
				} else {
					observation, err = controller.Abort(ctx, host, call, *request)
				}
			case *configurationv1.StatusRequest:
				observation, err = controller.Status(ctx, host, call, *request)
			default:
				err = errors.New("configuration.v1 请求类型无效")
			}
			if err != nil {
				return controllerError("configuration.controller.rejected", err)
			}
			if err := configurationv1.ValidateObservation(observation); err != nil {
				return controllerError("configuration.controller.invalid_response", err)
			}
			payload, err := json.Marshal(observation)
			if err != nil {
				return nil, nil, err
			}
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, payload, nil
		}
	}
	return sdk.Contribution{
		ExtensionPoint: configurationv1.ExtensionPoint, ID: capability,
		Descriptor: []byte(`{"protocol":"configuration.v1"}`),
		Handlers: map[string]sdk.Handler{
			configurationv1.OperationPrepare: handler(configurationv1.OperationPrepare),
			configurationv1.OperationCommit:  handler(configurationv1.OperationCommit),
			configurationv1.OperationAbort:   handler(configurationv1.OperationAbort),
			configurationv1.OperationStatus:  handler(configurationv1.OperationStatus),
		},
	}, nil
}

func controllerError(code string, err error) (*contractv1.CallResult, []byte, error) {
	return &contractv1.CallResult{
		Status: contractv1.CallResult_STATUS_ERROR,
		Error:  &contractv1.Error{Code: code, Message: err.Error()},
	}, nil, nil
}
