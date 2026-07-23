package assessmentprovider

import (
	"context"
	"encoding/json"

	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func (s *Service) Contribution() sdk.Contribution {
	descriptor := []byte(`{"title":"插件制品安全评估 Provider","subcommands":[{"name":"assessAdmission","description":"扫描精确制品并签署不可变准入记录"}]}`)
	return sdk.Contribution{ExtensionPoint: extpoint.ToolPackage, ID: Capability, Descriptor: descriptor, Handlers: map[string]sdk.Handler{
		"assessAdmission": func(ctx context.Context, host sdk.Host, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
			raw, err := s.AssessAdmission(ctx, host, callCtx, payload)
			if err != nil {
				return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: "platform.artifacts.assessment.failed", Message: err.Error(), Retryable: false}}, nil, nil
			}
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
		},
	}}
}

func MarshalConfig(value Config) ([]byte, error) { return json.Marshal(value) }
