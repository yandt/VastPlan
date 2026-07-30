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
	case ProviderOperationCreateTag:
		target = &ProviderCreateTagRequest{}
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
	if request, ok := target.(*ProviderCreateTagRequest); ok {
		return target, validateProviderCreateTagRequest(*request)
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
	case OperationListHeads:
		return &ListHeadsRequest{}
	case OperationCreateHead:
		return &CreateHeadRequest{}
	case OperationMoveHead:
		return &MoveHeadRequest{}
	case OperationDeleteHead:
		return &DeleteHeadRequest{}
	case OperationCreateTag:
		return &CreateTagRequest{}
	case OperationGetTag:
		return &GetTagRequest{}
	case OperationListTags:
		return &ListTagsRequest{}
	case OperationCompare:
		return &CompareVersionsRequest{}
	case OperationIsAncestor:
		return &IsAncestorRequest{}
	case OperationCommonAncestor:
		return &FindCommonAncestorRequest{}
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
	case OperationListHeads:
		return &ListHeadsResult{}
	case OperationCreateHead:
		return &CreateHeadResult{}
	case OperationMoveHead:
		return &MoveHeadResult{}
	case OperationDeleteHead:
		return &DeleteHeadResult{}
	case OperationCreateTag:
		return &CreateTagResult{}
	case OperationGetTag:
		return &GetTagResult{}
	case OperationListTags:
		return &ListTagsResult{}
	case OperationCompare:
		return &CompareVersionsResult{}
	case OperationIsAncestor:
		return &IsAncestorResult{}
	case OperationCommonAncestor:
		return &FindCommonAncestorResult{}
	default:
		return nil
	}
}
