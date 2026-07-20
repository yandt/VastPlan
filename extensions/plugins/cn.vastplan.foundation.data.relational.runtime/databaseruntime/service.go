package databaseruntime

import (
	"context"
	"encoding/json"
	"fmt"

	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

// Service intentionally exposes only Provider discovery in phase 1. Pool and
// credential operations remain fail-closed until trusted runtime identity is
// implemented; advertising unfinished operations would create a false API.
type Service struct{ registry *Registry }

func NewService(registry *Registry) (*Service, error) {
	if registry == nil {
		return nil, fmt.Errorf("Database Provider Registry 不能为空")
	}
	return &Service{registry: registry}, nil
}

func (s *Service) Providers(payload []byte) (databasev1.ProviderListResult, error) {
	if _, err := databasev1.ParseRequest(databasev1.OperationProviders, payload); err != nil {
		return databasev1.ProviderListResult{}, err
	}
	return databasev1.ProviderListResult{Providers: s.registry.Descriptors()}, nil
}

func (s *Service) providerHandler(_ context.Context, _ sdk.Host, _ *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
	result, err := s.Providers(payload)
	if err != nil {
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{
			Code: databasev1.ErrorInvalidRequest, Message: err.Error(),
		}}, nil, nil
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, nil, err
	}
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
}

func (s *Service) Contribution() sdk.Contribution {
	descriptor := []byte(`{"title":"Database Runtime","subcommands":[{"name":"providers","description":"列出当前制品内已注册的关系数据库 Provider","paramsSchema":{"type":"object","additionalProperties":false,"maxProperties":0}}]}`)
	return sdk.Contribution{
		ExtensionPoint: extpoint.ToolPackage, ID: databasev1.Capability, Descriptor: descriptor,
		Handlers: map[string]sdk.Handler{databasev1.OperationProviders: s.providerHandler},
	}
}
