package databaseruntime

import (
	"errors"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
	recordstorev1 "cdsoft.com.cn/VastPlan/contracts/schemas/recordstore/v1"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.foundation.data.relational.runtime/databaseruntime/recordstore"
)

func recordResult(value any, err error) (*contractv1.CallResult, []byte, error) {
	if err == nil {
		return runtimeResult(value, nil)
	}
	code, retryable := recordErrorDetails(err)
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{
		Code: code, Message: recordSafeMessage(code), Retryable: retryable,
	}}, nil, nil
}

func recordErrorDetails(err error) (string, bool) {
	switch {
	case errors.Is(err, recordstore.ErrNotFound):
		return recordstorev1.ErrorNotFound, false
	case errors.Is(err, recordstore.ErrAlreadyExists):
		return recordstorev1.ErrorAlreadyExists, false
	case errors.Is(err, recordstore.ErrConflict):
		return recordstorev1.ErrorConflict, false
	case errors.Is(err, recordstore.ErrMigrationNeeded):
		return recordstorev1.ErrorMigrationNeeded, false
	case errors.Is(err, recordstore.ErrModelNotFound):
		return recordstorev1.ErrorModelNotFound, false
	case errors.Is(err, recordstore.ErrModelMismatch):
		return recordstorev1.ErrorModelMismatch, false
	case errors.Is(err, recordstore.ErrStorageDenied):
		return recordstorev1.ErrorStorageDenied, false
	}
	databaseCode, retryable := ErrorDetails(err)
	switch databaseCode {
	case databasev1.ErrorTransactionLost:
		return recordstorev1.ErrorTransactionLost, true
	case databasev1.ErrorTransactionExpired:
		return recordstorev1.ErrorTransactionExpired, false
	case databasev1.ErrorTransactionConflict:
		return recordstorev1.ErrorConflict, retryable
	case databasev1.ErrorConstraintViolation:
		return recordstorev1.ErrorConflict, false
	case databasev1.ErrorInvalidRequest, databasev1.ErrorUnsupported:
		return recordstorev1.ErrorInvalidRequest, false
	default:
		return recordstorev1.ErrorUnavailable, retryable
	}
}

func recordSafeMessage(code string) string {
	switch code {
	case recordstorev1.ErrorNotFound:
		return "记录不存在"
	case recordstorev1.ErrorAlreadyExists:
		return "记录已存在"
	case recordstorev1.ErrorConflict:
		return "记录已被其他操作更新"
	case recordstorev1.ErrorMigrationNeeded:
		return "数据模型尚未完成迁移"
	case recordstorev1.ErrorTransactionLost:
		return "数据库事务已丢失"
	case recordstorev1.ErrorTransactionExpired:
		return "数据库事务已过期"
	case recordstorev1.ErrorStorageDenied:
		return "当前调用无权使用该存储绑定"
	case recordstorev1.ErrorModelNotFound, recordstorev1.ErrorModelMismatch:
		return "数据模型不可用"
	case recordstorev1.ErrorUnavailable:
		return "记录存储暂时不可用"
	default:
		return "记录存储请求无效"
	}
}
