package versionworkspace

import (
	"context"
	"errors"

	versioningv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versioning/v1"
	workspacev1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionworkspace/v1"
)

type WorkspaceError struct {
	Code      string
	Retryable bool
	Err       error
}

func (e *WorkspaceError) Error() string {
	if e == nil || e.Err == nil {
		return "Version Workspace 操作失败"
	}
	return e.Err.Error()
}

func (e *WorkspaceError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func workspaceError(code string, retryable bool, err error) error {
	if err == nil {
		err = errors.New("Version Workspace 操作失败")
	}
	if !workspacev1.KnownErrorCode(code) {
		code = workspacev1.ErrorAdapterUnavailable
	}
	return &WorkspaceError{Code: code, Retryable: retryable, Err: err}
}

func ErrorDetails(err error) (string, bool) {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return workspacev1.ErrorLedgerUnavailable, true
	}
	var workspaceErr *WorkspaceError
	if errors.As(err, &workspaceErr) {
		return workspaceErr.Code, workspaceErr.Retryable
	}
	return workspacev1.ErrorAdapterUnavailable, false
}

func mapLedgerFailure(err error, notFoundCode string) error {
	var ledgerErr *LedgerError
	if !errors.As(err, &ledgerErr) {
		return workspaceError(workspacev1.ErrorLedgerUnavailable, true, err)
	}
	switch ledgerErr.Code {
	case versioningv1.ErrorNotFound:
		return workspaceError(notFoundCode, false, err)
	case versioningv1.ErrorConflict:
		return workspaceError(workspacev1.ErrorBaseConflict, false, err)
	case versioningv1.ErrorLimitExceeded:
		return workspaceError(workspacev1.ErrorLimitExceeded, false, err)
	case versioningv1.ErrorInvalidRequest, versioningv1.ErrorDigestMismatch, versioningv1.ErrorCorrupted:
		return workspaceError(workspacev1.ErrorLedgerUnavailable, false, err)
	default:
		return workspaceError(workspacev1.ErrorLedgerUnavailable, ledgerErr.Retryable, err)
	}
}

func isLedgerCode(err error, code string) bool {
	var ledgerErr *LedgerError
	return errors.As(err, &ledgerErr) && ledgerErr.Code == code
}
