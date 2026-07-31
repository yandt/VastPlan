package contentstaging

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/extpoint"
	contentv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versioncontent/v1"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func (s *Service) contentHandle(operation string) sdk.Handler {
	return func(ctx context.Context, _ sdk.Host, call *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
		parsed, err := contentv1.ParseRequest(operation, payload)
		if err != nil {
			return contentServiceResult(operation, contentv1.ProtectionResult{}, contentReferenceError(contentv1.ErrorInvalidRequest, false, err))
		}
		scope, err := requestScope(call)
		if err != nil {
			return contentServiceResult(operation, contentv1.ProtectionResult{}, contentReferenceError(contentv1.ErrorInvalidRequest, false, err))
		}
		var result contentv1.ProtectionResult
		switch request := parsed.(type) {
		case *contentv1.PrepareRequest:
			result, err = s.manager.PrepareProtection(ctx, scope, *request)
		case *contentv1.StatusRequest:
			result, err = s.manager.ProtectionStatus(ctx, scope, *request)
		case *contentv1.ConfirmRequest:
			result, err = s.manager.ConfirmProtection(ctx, scope, *request)
		case *contentv1.AbortRequest:
			result, err = s.manager.AbortProtection(ctx, scope, *request)
		default:
			err = contentReferenceError(contentv1.ErrorUnsupported, false, errors.New("Content Reference 操作未实现"))
		}
		return contentServiceResult(operation, result, err)
	}
}

func contentServiceResult(operation string, result contentv1.ProtectionResult, err error) (*contractv1.CallResult, []byte, error) {
	if err != nil {
		code, retryable := ContentReferenceErrorDetails(err)
		message := "Content Reference 操作失败"
		if contentv1.KnownErrorCode(code) {
			message = err.Error()
		}
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: code, Message: message, Retryable: retryable}}, nil, nil
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, nil, err
	}
	if _, err := contentv1.ParseResult(operation, raw); err != nil {
		return nil, nil, fmt.Errorf("Content Reference 生成无效结果: %w", err)
	}
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
}

func (s *Service) ContentReferenceContribution() sdk.Contribution {
	return sdk.Contribution{
		ExtensionPoint: extpoint.ToolPackage, ID: contentv1.Capability, Descriptor: contentReferenceDescriptor(),
		Handlers: map[string]sdk.Handler{
			contentv1.OperationPrepare: s.contentHandle(contentv1.OperationPrepare), contentv1.OperationStatus: s.contentHandle(contentv1.OperationStatus),
			contentv1.OperationConfirm: s.contentHandle(contentv1.OperationConfirm), contentv1.OperationAbort: s.contentHandle(contentv1.OperationAbort),
		},
	}
}

func contentReferenceDescriptor() []byte {
	return []byte(`{"title":"Version Content Reference","subcommands":[{"name":"prepareVersion","description":"在 Ledger 提交前持久保护 manifest 内容"},{"name":"protectionStatus","description":"读取持久保护状态"},{"name":"confirmVersion","description":"将保护幂等确认给精确 VersionRef"},{"name":"abortProtection","description":"显式终止未确认保护"}]}`)
}
