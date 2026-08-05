package recordstore

import (
	"errors"

	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
)

var (
	ErrNotFound           = errors.New("record store entry not found")
	ErrConflict           = errors.New("record store revision conflict")
	ErrAlreadyExists      = errors.New("record store entry already exists")
	ErrModelNotFound      = errors.New("record store model not found")
	ErrModelMismatch      = errors.New("record store model identity mismatch")
	ErrStorageDenied      = errors.New("record store storage binding denied")
	ErrStorageUnavailable = errors.New("record store storage binding unavailable")
	ErrMigrationNeeded    = errors.New("record store schema migration needed")
)

type runtimeCoded interface{ RuntimeCode() string }

func constraintViolation(err error) bool {
	var coded runtimeCoded
	return errors.As(err, &coded) && coded.RuntimeCode() == databasev1.ErrorConstraintViolation
}
