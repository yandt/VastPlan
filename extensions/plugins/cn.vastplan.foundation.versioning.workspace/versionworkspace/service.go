package versionworkspace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/extpoint"
	workspacev1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionworkspace/v1"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

type Service struct{ manager *Manager }

func NewService(manager *Manager) *Service { return &Service{manager: manager} }

func requestScope(call *contractv1.CallContext) (Scope, error) {
	if call == nil || call.GetCaller() == nil || call.GetCaller().GetId() == "" || call.GetTenantId() == "" {
		return Scope{}, errors.New("Version Workspace 调用缺少可信 caller 或 tenant")
	}
	prefix := ""
	switch call.GetCaller().GetKind() {
	case contractv1.CallerKind_CALLER_KIND_PLUGIN:
		prefix = "plugin:"
	case contractv1.CallerKind_CALLER_KIND_SYSTEM:
		prefix = "system:"
	default:
		return Scope{}, errors.New("只有插件或系统主体可以直接调用 Version Workspace")
	}
	scope := Scope{TenantID: call.GetTenantId(), ActorID: prefix + call.GetCaller().GetId()}
	return scope, scope.Validate()
}

func (s *Service) handle(operation string) sdk.Handler {
	return func(ctx context.Context, host sdk.Host, call *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
		parsed, err := workspacev1.ParseRequest(operation, payload)
		if err != nil {
			return serviceResult(operation, nil, workspaceError(workspacev1.ErrorInvalidRequest, false, err))
		}
		scope, err := requestScope(call)
		if err != nil {
			return serviceResult(operation, nil, workspaceError(workspacev1.ErrorInvalidRequest, false, err))
		}
		var value any
		switch request := parsed.(type) {
		case *workspacev1.OpenRequest:
			ledger, callErr := newHostLedger(host, call)
			if callErr == nil {
				var session workspacev1.Session
				session, callErr = s.manager.Open(ctx, scope, ledger, *request)
				value = workspacev1.SessionResult{Session: session}
			}
			err = callErr
		case *workspacev1.SessionRequest:
			switch operation {
			case workspacev1.OperationStatus:
				var session workspacev1.Session
				session, err = s.manager.Status(scope, *request)
				value = workspacev1.SessionResult{Session: session}
			case workspacev1.OperationReadSnapshot:
				value, err = s.manager.ReadSnapshot(scope, *request)
			case workspacev1.OperationChanges:
				value, err = s.manager.Changes(ctx, scope, *request)
			}
		case *workspacev1.WriteSnapshotRequest:
			var session workspacev1.Session
			session, err = s.manager.WriteSnapshot(ctx, scope, *request)
			value = workspacev1.SessionResult{Session: session}
		case *workspacev1.CommitRequest:
			ledger, callErr := newHostLedger(host, call)
			if callErr == nil {
				value, callErr = s.manager.Commit(ctx, scope, ledger, *request)
			}
			err = callErr
		case *workspacev1.RevisionRequest:
			var session workspacev1.Session
			session, err = s.manager.Discard(scope, *request)
			value = workspacev1.SessionResult{Session: session}
		case *workspacev1.RenewRequest:
			var session workspacev1.Session
			session, err = s.manager.Renew(scope, *request)
			value = workspacev1.SessionResult{Session: session}
		default:
			err = workspaceError(workspacev1.ErrorInvalidRequest, false, errors.New("Version Workspace 操作未实现"))
		}
		return serviceResult(operation, value, err)
	}
}

func serviceResult(operation string, value any, err error) (*contractv1.CallResult, []byte, error) {
	if err != nil {
		code, retryable := ErrorDetails(err)
		message := "Version Workspace 操作失败"
		switch code {
		case workspacev1.ErrorInvalidRequest, workspacev1.ErrorEnvironmentNotFound, workspacev1.ErrorResourceNotBound,
			workspacev1.ErrorSessionNotFound, workspacev1.ErrorSessionConflict, workspacev1.ErrorLeaseExpired,
			workspacev1.ErrorReadOnly, workspacev1.ErrorLimitExceeded, workspacev1.ErrorBaseConflict:
			message = err.Error()
		}
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: code, Message: message, Retryable: retryable}}, nil, nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, nil, err
	}
	if _, err := workspacev1.ParseResult(operation, raw); err != nil {
		return nil, nil, fmt.Errorf("Version Workspace 生成无效结果: %w", err)
	}
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
}

func (s *Service) Contribution() sdk.Contribution {
	return sdk.Contribution{
		ExtensionPoint: extpoint.ToolPackage, ID: workspacev1.Capability, Descriptor: serviceDescriptor(),
		Handlers: map[string]sdk.Handler{
			workspacev1.OperationOpen: s.handle(workspacev1.OperationOpen), workspacev1.OperationStatus: s.handle(workspacev1.OperationStatus),
			workspacev1.OperationReadSnapshot: s.handle(workspacev1.OperationReadSnapshot), workspacev1.OperationWriteSnapshot: s.handle(workspacev1.OperationWriteSnapshot),
			workspacev1.OperationChanges: s.handle(workspacev1.OperationChanges), workspacev1.OperationCommit: s.handle(workspacev1.OperationCommit),
			workspacev1.OperationDiscard: s.handle(workspacev1.OperationDiscard), workspacev1.OperationRenew: s.handle(workspacev1.OperationRenew),
		},
	}
}

func serviceDescriptor() []byte {
	return []byte(`{"title":"Version Workspace","subcommands":[{"name":"open","description":"打开有 Lease 的版本编辑会话"},{"name":"status","description":"读取会话状态"},{"name":"readSnapshot","description":"读取隔离快照"},{"name":"writeSnapshot","description":"以 CAS 写入隔离快照"},{"name":"changes","description":"读取确定性变更摘要"},{"name":"commit","description":"幂等提交版本并以 CAS 更新 Head"},{"name":"discard","description":"丢弃编辑会话"},{"name":"renew","description":"在环境配额内续租"}]}`)
}
