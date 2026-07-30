package versioningv1

import (
	"errors"
	"fmt"
)

func ParseRequest(operation string, raw []byte) (any, error) {
	definition, ok := requestDefinitions[operation]
	if !ok {
		return nil, fmt.Errorf("不支持的 Version Ledger 操作 %q", operation)
	}
	target := requestTarget(operation)
	if err := decodeDefinition(definition, raw, target); err != nil {
		return nil, err
	}
	if err := validateRequest(target); err != nil {
		return nil, err
	}
	return target, nil
}

func ParseResult(operation string, raw []byte) (any, error) {
	definition, ok := resultDefinitions[operation]
	if !ok {
		return nil, fmt.Errorf("不支持的 Version Ledger 结果 %q", operation)
	}
	target := resultTarget(operation)
	if err := decodeDefinition(definition, raw, target); err != nil {
		return nil, err
	}
	if err := validateResult(target); err != nil {
		return nil, err
	}
	return target, nil
}

func ParseProviderRequest(operation string, raw []byte) (any, error) {
	definition, ok := providerRequestDefinitions[operation]
	if !ok {
		return nil, fmt.Errorf("不支持的 Version Provider 操作 %q", operation)
	}
	var target any
	switch operation {
	case ProviderOperationDescribe:
		target = &ProviderDescribeRequest{}
	case ProviderOperationPutVersion:
		target = &ProviderPutVersionRequest{}
	default:
		target = requestTarget(operation)
	}
	if err := decodeDefinition(definition, raw, target); err != nil {
		return nil, err
	}
	if request, ok := target.(*ProviderPutVersionRequest); ok {
		if !idempotencyPattern.MatchString(request.IdempotencyKey) {
			return nil, errors.New("Provider idempotencyKey 无效")
		}
		if err := ValidateProviderVersionCandidate(&request.Candidate); err != nil {
			return nil, err
		}
		return target, nil
	}
	return target, validateRequest(target)
}

func ParseProviderResult(operation string, raw []byte) (any, error) {
	definition, ok := providerResultDefinitions[operation]
	if !ok {
		return nil, fmt.Errorf("不支持的 Version Provider 结果 %q", operation)
	}
	if operation == ProviderOperationDescribe {
		var result ProviderDescribeResult
		if err := decodeDefinition(definition, raw, &result); err != nil {
			return nil, err
		}
		return &result, ValidateProviderDescriptor(result.Provider)
	}
	target := resultTarget(operation)
	if err := decodeDefinition(definition, raw, target); err != nil {
		return nil, err
	}
	return target, validateResult(target)
}

func requestTarget(operation string) any {
	switch operation {
	case OperationProviders:
		return &ProviderListRequest{}
	case OperationPutVersion:
		return &PutVersionRequest{}
	case OperationGetVersion:
		return &GetVersionRequest{}
	case OperationListHistory:
		return &ListHistoryRequest{}
	case OperationGetHead:
		return &GetHeadRequest{}
	case OperationMoveHead:
		return &MoveHeadRequest{}
	default:
		return nil
	}
}

func resultTarget(operation string) any {
	switch operation {
	case OperationProviders:
		return &ProviderListResult{}
	case OperationPutVersion:
		return &PutVersionResult{}
	case OperationGetVersion:
		return &GetVersionResult{}
	case OperationListHistory:
		return &ListHistoryResult{}
	case OperationGetHead:
		return &GetHeadResult{}
	case OperationMoveHead:
		return &MoveHeadResult{}
	default:
		return nil
	}
}
