package authorizationpolicy

import (
	"context"

	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

type operationDescriptor struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

var authorizationOperations = []operationDescriptor{
	{Name: "get", Description: "读取权限目录、角色和绑定"},
	{Name: "listAudit", Description: "读取授权审计"},
	{Name: "createRole", Description: "创建角色 revision"},
	{Name: "updateRole", Description: "修改 Draft 角色"},
	{Name: "submitRole", Description: "提交角色审批"},
	{Name: "approveRole", Description: "批准角色"},
	{Name: "publishRole", Description: "发布角色"},
	{Name: "retireRole", Description: "退役角色"},
	{Name: "createBinding", Description: "创建主体绑定"},
	{Name: "updateBinding", Description: "修改 Draft 主体绑定"},
	{Name: "submitBinding", Description: "提交绑定审批"},
	{Name: "approveBinding", Description: "批准绑定"},
	{Name: "publishBinding", Description: "发布绑定"},
	{Name: "retireBinding", Description: "退役绑定"},
	{Name: "revoke", Description: "即时撤权"},
	{Name: "publishSnapshot", Description: "签发策略快照"},
}

func (s *Service) Contribution() sdk.Contribution {
	handlers := make(map[string]sdk.Handler, len(authorizationOperations))
	for _, operation := range authorizationOperations {
		name := operation.Name
		handlers[name] = func(ctx context.Context, _ sdk.Host, callCtx *contractv1.CallContext, raw []byte) (*contractv1.CallResult, []byte, error) {
			return s.handle(ctx, callCtx, name, raw)
		}
	}
	return sdk.Contribution{
		ExtensionPoint: extpoint.ToolPackage,
		ID:             Capability,
		Descriptor:     contributionDescriptor(),
		Handlers:       handlers,
	}
}

func contributionDescriptor() []byte {
	return mustJSON(map[string]any{
		"title":       "在线角色与权限策略",
		"subcommands": authorizationOperations,
	})
}
