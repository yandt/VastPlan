package versionledger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/extpoint"
	versioningv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versioning/v1"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

type Service struct {
	registry        *Registry
	defaultProvider string
	routes          map[string]string
}

func NewService(registry *Registry, defaultProvider string, routes []ProviderRoute) (*Service, error) {
	resolved, err := validateRoutes(registry, defaultProvider, routes)
	if err != nil {
		return nil, err
	}
	return &Service{registry: registry, defaultProvider: defaultProvider, routes: resolved}, nil
}

func (s *Service) provider(namespace string) (Provider, error) {
	instanceID := s.defaultProvider
	if routed := s.routes[namespace]; routed != "" {
		instanceID = routed
	}
	provider, ok := s.registry.Resolve(instanceID)
	if !ok {
		return nil, providerError(versioningv1.ErrorProviderNotFound, false, errors.New("Version Provider route 不可用"))
	}
	return provider, nil
}

func requestScope(call *contractv1.CallContext) (Scope, string, error) {
	if call == nil || call.GetCaller() == nil || call.GetCaller().GetId() == "" || call.GetTenantId() == "" {
		return Scope{}, "", errors.New("Version Ledger 调用缺少可信 caller 或 tenant")
	}
	prefix := ""
	switch call.GetCaller().GetKind() {
	case contractv1.CallerKind_CALLER_KIND_PLUGIN:
		prefix = "plugin:"
	case contractv1.CallerKind_CALLER_KIND_SYSTEM:
		prefix = "system:"
	default:
		return Scope{}, "", errors.New("只有插件或系统主体可以直接调用 Version Ledger")
	}
	actorID := prefix + call.GetCaller().GetId()
	if len(actorID) > 160 {
		return Scope{}, "", errors.New("Version Ledger actor 超过长度限制")
	}
	return Scope{TenantID: call.GetTenantId()}, actorID, nil
}

func (s *Service) handle(operation string) sdk.Handler {
	return func(ctx context.Context, _ sdk.Host, call *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
		parsed, err := versioningv1.ParseRequest(operation, payload)
		if err != nil {
			return serviceResult(operation, nil, providerError(versioningv1.ErrorInvalidRequest, false, err))
		}
		if _, ok := parsed.(*versioningv1.ProviderListRequest); ok {
			if _, _, err := requestScope(call); err != nil {
				return serviceResult(operation, nil, providerError(versioningv1.ErrorInvalidRequest, false, err))
			}
			return serviceResult(operation, versioningv1.ProviderListResult{Providers: s.registry.Descriptors()}, nil)
		}
		scope, actorID, err := requestScope(call)
		if err != nil {
			return serviceResult(operation, nil, providerError(versioningv1.ErrorInvalidRequest, false, err))
		}
		switch request := parsed.(type) {
		case *versioningv1.PutVersionRequest:
			provider, callErr := s.provider(request.Stream.Namespace)
			if callErr != nil {
				return serviceResult(operation, nil, callErr)
			}
			candidate := versioningv1.ProviderVersionCandidate{
				Stream: request.Stream, Parent: request.Parent, Content: request.Content, Message: request.Message,
				Labels: request.Labels, ActorID: actorID,
			}
			value, callErr := provider.PutVersion(ctx, scope, versioningv1.ProviderPutVersionRequest{IdempotencyKey: request.IdempotencyKey, Candidate: candidate})
			if callErr == nil {
				callErr = validateProviderPutResult(candidate, value)
			}
			return serviceResult(operation, value, callErr)
		case *versioningv1.GetVersionRequest:
			provider, callErr := s.provider(request.Ref.Stream.Namespace)
			if callErr != nil {
				return serviceResult(operation, nil, callErr)
			}
			value, callErr := provider.GetVersion(ctx, scope, *request)
			if callErr == nil && value.Version.Ref != request.Ref {
				callErr = providerError(versioningv1.ErrorCorrupted, false, errors.New("Provider 返回了错误的版本引用"))
			}
			return serviceResult(operation, value, callErr)
		case *versioningv1.ListHistoryRequest:
			provider, callErr := s.provider(request.Stream.Namespace)
			if callErr != nil {
				return serviceResult(operation, nil, callErr)
			}
			value, callErr := provider.ListHistory(ctx, scope, *request)
			if callErr == nil {
				callErr = validateProviderHistory(*request, value)
			}
			return serviceResult(operation, value, callErr)
		case *versioningv1.GetHeadRequest:
			provider, callErr := s.provider(request.Stream.Namespace)
			if callErr != nil {
				return serviceResult(operation, nil, callErr)
			}
			value, callErr := provider.GetHead(ctx, scope, *request)
			if callErr == nil && (value.Head.Stream != request.Stream || value.Head.Name != request.Name) {
				callErr = providerError(versioningv1.ErrorCorrupted, false, errors.New("Provider 返回了错误的 Version Head"))
			}
			return serviceResult(operation, value, callErr)
		case *versioningv1.MoveHeadRequest:
			provider, callErr := s.provider(request.Stream.Namespace)
			if callErr != nil {
				return serviceResult(operation, nil, callErr)
			}
			value, callErr := provider.MoveHead(ctx, scope, *request)
			if callErr == nil && (value.Head.Stream != request.Stream || value.Head.Name != request.Name || value.Head.Target != request.Target || value.Head.Revision != request.ExpectedRevision+1) {
				callErr = providerError(versioningv1.ErrorCorrupted, false, errors.New("Provider 返回了错误的 Head CAS 结果"))
			}
			return serviceResult(operation, value, callErr)
		default:
			return serviceResult(operation, nil, providerError(versioningv1.ErrorUnsupported, false, errors.New("Version Ledger 操作尚未开放")))
		}
	}
}

func validateProviderHistory(request versioningv1.ListHistoryRequest, result versioningv1.ListHistoryResult) error {
	for index, record := range result.Versions {
		if record.Ref.Stream != request.Stream {
			return providerError(versioningv1.ErrorCorrupted, false, errors.New("Provider 历史结果跨越 stream"))
		}
		if index == 0 && request.Start != nil && record.Ref != *request.Start {
			return providerError(versioningv1.ErrorCorrupted, false, errors.New("Provider 历史结果起点不匹配"))
		}
	}
	return nil
}

func validateProviderPutResult(candidate versioningv1.ProviderVersionCandidate, result versioningv1.PutVersionResult) error {
	if err := versioningv1.ValidateVersionRecord(result.Version); err != nil {
		return providerError(versioningv1.ErrorCorrupted, false, fmt.Errorf("Provider 返回无效版本: %w", err))
	}
	if !sameCandidate(result.Version, candidate) {
		return providerError(versioningv1.ErrorCorrupted, false, errors.New("Provider 返回版本与候选不一致"))
	}
	return nil
}

func serviceResult(operation string, value any, err error) (*contractv1.CallResult, []byte, error) {
	if err != nil {
		code, retryable := errorDetails(err)
		message := "Version Ledger 操作失败"
		switch code {
		case versioningv1.ErrorInvalidRequest, versioningv1.ErrorNotFound, versioningv1.ErrorConflict, versioningv1.ErrorLimitExceeded, versioningv1.ErrorUnsupported:
			message = err.Error()
		}
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: code, Message: message, Retryable: retryable}}, nil, nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, nil, err
	}
	if _, err := versioningv1.ParseResult(operation, raw); err != nil {
		return nil, nil, fmt.Errorf("Version Ledger 生成无效结果: %w", err)
	}
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
}

func (s *Service) Contribution() sdk.Contribution {
	return sdk.Contribution{
		ExtensionPoint: extpoint.ToolPackage, ID: versioningv1.LedgerCapability, Descriptor: serviceDescriptor(),
		Handlers: map[string]sdk.Handler{
			versioningv1.OperationProviders:   s.handle(versioningv1.OperationProviders),
			versioningv1.OperationPutVersion:  s.handle(versioningv1.OperationPutVersion),
			versioningv1.OperationGetVersion:  s.handle(versioningv1.OperationGetVersion),
			versioningv1.OperationListHistory: s.handle(versioningv1.OperationListHistory),
			versioningv1.OperationGetHead:     s.handle(versioningv1.OperationGetHead),
			versioningv1.OperationMoveHead:    s.handle(versioningv1.OperationMoveHead),
		},
	}
}

func serviceDescriptor() []byte {
	return []byte(`{"title":"Version Ledger","subcommands":[{"name":"providers","description":"列出已注册的版本存储 Provider 类型","paramsSchema":{"type":"object","additionalProperties":false,"maxProperties":0}},{"name":"putVersion","description":"幂等创建不可变版本"},{"name":"getVersion","description":"精确读取不可变版本"},{"name":"listHistory","description":"沿父链分页读取版本历史"},{"name":"getHead","description":"读取命名 Head"},{"name":"moveHead","description":"以 CAS 移动命名 Head"}]}`)
}
