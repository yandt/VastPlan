package controller

import (
	"context"
	"encoding/json"

	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func (c *Controller) Contribution() sdk.Contribution {
	descriptor := []byte(`{"title":"插件制品持续复扫 Controller","subcommands":[{"name":"status","description":"读取低基数调度状态"}]}`)
	return sdk.Contribution{ExtensionPoint: extpoint.ToolPackage, ID: Capability, Descriptor: descriptor, Handlers: map[string]sdk.Handler{
		"status": func(context.Context, sdk.Host, *contractv1.CallContext, []byte) (*contractv1.CallResult, []byte, error) {
			raw, err := json.Marshal(c.Stats())
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, err
		},
	}}
}
