package main

import (
	"context"
	"encoding/json"
	"errors"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/extpoint"
	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
	platformcontrolv1 "cdsoft.com.cn/VastPlan/contracts/schemas/platformcontrol/v1"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

const (
	operationPlatformControlStatus    = "platformControlStatus"
	operationPlatformControlTest      = "platformControlTest"
	operationPlatformControlConfigure = "platformControlConfigure"
)

func isPlatformControlOperation(operation string) bool {
	return operation == operationPlatformControlStatus || operation == operationPlatformControlTest || operation == operationPlatformControlConfigure
}

func callPlatformControl(ctx context.Context, host sdk.Host, call *contractv1.CallContext, operation string, payload []byte) (*contractv1.CallResult, []byte, error) {
	capability, kernelOperation := "", ""
	switch operation {
	case operationPlatformControlStatus:
		capability, kernelOperation = platformcontrolv1.KernelStatusService, "status"
		payload = []byte(`{}`)
	case operationPlatformControlTest:
		capability, kernelOperation = platformcontrolv1.KernelTestService, "test"
	case operationPlatformControlConfigure:
		capability, kernelOperation = platformcontrolv1.KernelConfigureService, "configure"
	default:
		return nil, nil, errors.New("不支持的 Platform Control 操作")
	}
	if operation != operationPlatformControlStatus {
		var request platformcontrolv1.ChangeRequest
		if err := json.Unmarshal(payload, &request); err != nil {
			return domainError("platform.database.platform_control_invalid", errors.New("Platform Control 配置无效"))
		}
		if err := platformcontrolv1.ValidateChangeRequest(request); err != nil {
			if issue, ok := platformcontrolv1.ValidationIssueFrom(err); ok {
				return domainErrorDetails("platform.database.platform_control_invalid", err, validationDetails(issue.Field, issue.Reason))
			}
			if issue, ok := databasev1.ValidationIssueFrom(err); ok {
				return domainErrorDetails("platform.database.platform_control_invalid", err, validationDetails(issue.Field, issue.Reason))
			}
			return domainError("platform.database.platform_control_invalid", errors.New("Platform Control 配置无效"))
		}
	}
	return host.Call(ctx, &contractv1.CallTarget{
		ExtensionPoint: extpoint.KernelService,
		Capability:     capability,
		Operation:      &kernelOperation,
	}, call, payload)
}
