package main

import (
	"context"
	"errors"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/extpoint"
	apiv1 "cdsoft.com.cn/VastPlan/contracts/schemas/api/v1"
	stagingv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionstaging/v1"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

type uploadTicketRegistry map[string]*uploadTicketStore

func (r uploadTicketRegistry) install(ctx context.Context, call *contractv1.CallContext, raw []byte) error {
	var installation apiv1.DataPlaneTicketInstallation
	if err := decodeUploadJSON(raw, &installation); err != nil {
		return err
	}
	store := r[installation.Claims.InstanceID]
	if store == nil {
		return errors.New("Content Upload Ticket 未命中运行实例")
	}
	return store.install(ctx, call, raw)
}

func uploadDataPlaneContribution(tickets uploadTicketRegistry) sdk.Contribution {
	return sdk.Contribution{
		ExtensionPoint: extpoint.ToolPackage, ID: stagingv1.UploadDataPlaneCapability, Descriptor: uploadDataPlaneDescriptor(),
		Handlers: map[string]sdk.Handler{
			stagingv1.OperationInstallDataPlaneTicket: func(ctx context.Context, _ sdk.Host, call *contractv1.CallContext, raw []byte) (*contractv1.CallResult, []byte, error) {
				if len(tickets) == 0 {
					return nil, nil, errors.New("Content Upload 数据面未启用")
				}
				if err := tickets.install(ctx, call, raw); err != nil {
					return nil, nil, err
				}
				return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, []byte(`{"installed":true}`), nil
			},
		},
	}
}

func uploadDataPlaneDescriptor() []byte {
	return []byte(`{"title":"Version Content Upload","subcommands":[{"name":"installDataPlaneTicket","description":"安装 API Exposure 签发的一次性内容上传 Ticket"}]}`)
}
