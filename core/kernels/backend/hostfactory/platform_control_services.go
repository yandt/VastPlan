package hostfactory

import (
	"context"
	"encoding/json"
	"errors"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	platformcontrolv1 "cdsoft.com.cn/VastPlan/contracts/schemas/platformcontrol/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/protocolbus"
	platformcontrol "cdsoft.com.cn/VastPlan/extensions/libraries/go/platformcontrol"
)

const databaseConnectionManagerID = "cn.vastplan.platform.data.relational.connection-manager"

func kernelPlatformControl(admin platformcontrol.Administration) map[string]protocolbus.HostService {
	status := func(_ context.Context, call *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
		if !authenticatedPlatformControlManager(call) {
			return nil, nil, errors.New("Platform Control 状态只接受数据库连接管理插件")
		}
		var empty struct{}
		if err := decodeStrict(payload, &empty); err != nil {
			return nil, nil, err
		}
		raw, err := json.Marshal(admin.Status())
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, err
	}
	change := func(configure bool) protocolbus.HostService {
		return func(ctx context.Context, call *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
			if !authenticatedPlatformControlManager(call) {
				return nil, nil, errors.New("Platform Control 修改只接受数据库连接管理插件")
			}
			var request platformcontrolv1.ChangeRequest
			if err := decodeStrict(payload, &request); err != nil {
				return nil, nil, err
			}
			var err error
			if configure {
				err = admin.Configure(ctx, request)
			} else {
				err = admin.TestCandidate(ctx, request)
			}
			if err != nil {
				return platformControlFailure(admin.Status(), err)
			}
			raw, marshalErr := json.Marshal(admin.Status())
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, marshalErr
		}
	}
	return map[string]protocolbus.HostService{
		platformcontrolv1.KernelStatusService:    status,
		platformcontrolv1.KernelTestService:      change(false),
		platformcontrolv1.KernelConfigureService: change(true),
	}
}

func authenticatedPlatformControlManager(call *contractv1.CallContext) bool {
	return call != nil && call.GetCaller().GetKind() == contractv1.CallerKind_CALLER_KIND_PLUGIN &&
		call.GetCaller().GetId() == databaseConnectionManagerID
}

func platformControlFailure(status platformcontrolv1.Status, _ error) (*contractv1.CallResult, []byte, error) {
	code := status.Code
	if code == "" {
		code = platformcontrolv1.ErrorUnavailable
	}
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{
		Code: code, Message: "Platform Control 请求失败", Retryable: code == platformcontrolv1.ErrorUnavailable,
	}}, nil, nil
}
