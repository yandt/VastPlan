package marketplace

import (
	"context"
	"encoding/json"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/extpoint"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/pluginmarketplace"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func (s *Service) Contribution() sdk.Contribution {
	return sdk.Contribution{ExtensionPoint: extpoint.ToolPackage, ID: pluginmarketplace.Capability, Descriptor: descriptor(), Handlers: map[string]sdk.Handler{
		pluginmarketplace.ListSourcesOp: func(context.Context, sdk.Host, *contractv1.CallContext, []byte) (*contractv1.CallResult, []byte, error) {
			raw, err := json.Marshal(s.Sources())
			return okResult(), raw, err
		},
		pluginmarketplace.ListCatalogOp: func(ctx context.Context, host sdk.Host, call *contractv1.CallContext, raw []byte) (*contractv1.CallResult, []byte, error) {
			var request pluginmarketplace.CatalogRequest
			if err := decodeStrict(raw, &request); err != nil {
				return errorResult("platform.marketplace.invalid_request", err), nil, nil
			}
			page, err := s.ListCatalog(ctx, host, call, request)
			if err != nil {
				return errorResult("platform.marketplace.catalog_unavailable", err), nil, nil
			}
			response, err := json.Marshal(page)
			return okResult(), response, err
		},
	}}
}

func descriptor() []byte {
	return []byte(`{"title":"插件市场","subcommands":[{"name":"listSources","description":"列出受治理市场来源"},{"name":"listCatalog","description":"查询一个受治理市场目录"}]}`)
}

func okResult() *contractv1.CallResult {
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}
}
func errorResult(code string, err error) *contractv1.CallResult {
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: code, Message: err.Error(), Retryable: code == "platform.marketplace.catalog_unavailable"}}
}
