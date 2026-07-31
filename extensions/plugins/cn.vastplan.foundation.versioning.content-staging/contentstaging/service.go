package contentstaging

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/extpoint"
	stagingv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionstaging/v1"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

const workspacePluginID = "cn.vastplan.foundation.versioning.workspace"

type Service struct{ manager *Manager }

func NewService(manager *Manager) *Service { return &Service{manager: manager} }

func requestScope(call *contractv1.CallContext) (Scope, error) {
	if call == nil || call.GetTenantId() == "" || call.GetCaller().GetKind() != contractv1.CallerKind_CALLER_KIND_PLUGIN || call.GetCaller().GetId() != workspacePluginID {
		return Scope{}, errors.New("只有可信 Version Workspace 可以调用 Content Staging 控制面")
	}
	actor := strings.TrimSpace(call.GetPrincipal().GetUserId())
	if actor != "" {
		actor = "user:" + actor
	} else {
		actor = "plugin:" + call.GetCaller().GetId()
	}
	scope := Scope{TenantID: call.GetTenantId(), ActorID: actor}
	return scope, scope.Validate()
}

func (s *Service) handle(operation string) sdk.Handler {
	return func(ctx context.Context, _ sdk.Host, call *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
		parsed, err := stagingv1.ParseRequest(operation, payload)
		if err != nil {
			return serviceResult(operation, stagingv1.UploadStatusResult{}, stagingError(stagingv1.ErrorInvalidRequest, false, err))
		}
		scope, err := requestScope(call)
		if err != nil {
			return serviceResult(operation, stagingv1.UploadStatusResult{}, stagingError(stagingv1.ErrorInvalidRequest, false, err))
		}
		var result stagingv1.UploadStatusResult
		switch request := parsed.(type) {
		case *stagingv1.BeginUploadRequest:
			result, err = s.manager.Begin(ctx, scope, call.GetIdempotencyKey(), *request)
		case *stagingv1.UploadStatusRequest:
			result, err = s.manager.Status(ctx, scope, *request)
		case *stagingv1.RenewUploadRequest:
			result, err = s.manager.Renew(ctx, scope, *request)
		case *stagingv1.UploadRevisionRequest:
			if operation == stagingv1.OperationCompleteUpload {
				result, err = s.manager.Complete(ctx, scope, *request)
			} else {
				result, err = s.manager.Abort(ctx, scope, *request)
			}
		default:
			err = stagingError(stagingv1.ErrorOperationUnsupported, false, errors.New("Content Staging 操作未实现"))
		}
		return serviceResult(operation, result, err)
	}
}

func serviceResult(operation string, result stagingv1.UploadStatusResult, err error) (*contractv1.CallResult, []byte, error) {
	if err != nil {
		code, retryable := ErrorDetails(err)
		message := "Content Staging 操作失败"
		if stagingv1.KnownErrorCode(code) {
			message = err.Error()
		}
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: code, Message: message, Retryable: retryable}}, nil, nil
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, nil, err
	}
	if _, err := stagingv1.ParseResult(operation, raw); err != nil {
		return nil, nil, fmt.Errorf("Content Staging 生成无效结果: %w", err)
	}
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
}

func (s *Service) Contribution() sdk.Contribution {
	return sdk.Contribution{
		ExtensionPoint: extpoint.ToolPackage, ID: stagingv1.Capability, Descriptor: serviceDescriptor(),
		Handlers: map[string]sdk.Handler{
			stagingv1.OperationBeginUpload: s.handle(stagingv1.OperationBeginUpload), stagingv1.OperationUploadStatus: s.handle(stagingv1.OperationUploadStatus),
			stagingv1.OperationRenewUpload: s.handle(stagingv1.OperationRenewUpload), stagingv1.OperationCompleteUpload: s.handle(stagingv1.OperationCompleteUpload),
			stagingv1.OperationAbortUpload: s.handle(stagingv1.OperationAbortUpload),
		},
	}
}

func (s *Service) RunReclaimer(ctx context.Context, interval time.Duration) {
	s.manager.RunReclaimer(ctx, interval)
}

func serviceDescriptor() []byte {
	return []byte(`{"title":"Version Content Staging","subcommands":[{"name":"beginUpload","description":"为可信 Workspace 创建绑定 Session 的上传 Lease"},{"name":"uploadStatus","description":"读取上传状态和接收进度"},{"name":"renewUpload","description":"以 CAS 续租临时内容保护"},{"name":"completeUpload","description":"封闭输入并校验摘要、大小与安全准入"},{"name":"abortUpload","description":"以 CAS 终止上传并进入安全回收"}]}`)
}
