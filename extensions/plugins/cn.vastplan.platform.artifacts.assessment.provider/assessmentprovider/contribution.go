package assessmentprovider

import (
	"context"
	"encoding/json"

	"cdsoft.com.cn/VastPlan/core/shared/go/artifactassessment"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	provider "cdsoft.com.cn/VastPlan/extensions/sdk/go/artifactassessmentprovider"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func (s *Service) Contribution() sdk.Contribution {
	descriptor := []byte(`{"title":"插件制品安全评估 Provider","subcommands":[{"name":"status","description":"读取扫描器与评估策略 revision"},{"name":"assessAdmission","description":"扫描精确制品并签署不可变准入记录"},{"name":"assessStatus","description":"复扫精确制品并签署只追加状态记录"}]}`)
	return sdk.Contribution{ExtensionPoint: extpoint.ToolPackage, ID: Capability, Descriptor: descriptor, Handlers: map[string]sdk.Handler{
		"status": func(_ context.Context, _ sdk.Host, callCtx *contractv1.CallContext, _ []byte) (*contractv1.CallResult, []byte, error) {
			if callCtx.GetCaller().GetKind() != contractv1.CallerKind_CALLER_KIND_PLUGIN || callCtx.GetCaller().GetId() != artifactassessment.AssessmentControllerPluginID {
				return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: "platform.artifacts.assessment.denied", Message: "仅允许 Assessment Controller 读取 Provider 状态"}}, nil, nil
			}
			value := artifactassessment.ProviderRuntimeStatus{SchemaVersion: artifactassessment.SchemaVersion, Scanner: artifactassessment.Scanner{ID: provider.DefaultScannerID, Version: s.config.ScannerVersion, DatabaseRevision: s.config.DatabaseRevision}, AssessmentRevision: s.config.AssessmentRevision()}
			raw, err := json.Marshal(value)
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, err
		},
		"assessAdmission": func(ctx context.Context, host sdk.Host, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
			raw, err := s.AssessAdmission(ctx, host, callCtx, payload)
			if err != nil {
				return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: "platform.artifacts.assessment.failed", Message: err.Error(), Retryable: false}}, nil, nil
			}
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
		},
		"assessStatus": func(ctx context.Context, host sdk.Host, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
			raw, err := s.AssessStatus(ctx, host, callCtx, payload)
			if err != nil {
				return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: "platform.artifacts.assessment.rescan_failed", Message: err.Error(), Retryable: true}}, nil, nil
			}
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
		},
	}}
}

func MarshalConfig(value Config) ([]byte, error) { return json.Marshal(value) }
