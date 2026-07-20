package databaseruntime

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"net"
	"strings"

	mysql "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"

	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
)

func classifySQLError(err error, transaction bool) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return NewRuntimeError(databasev1.ErrorDeadlineExceeded, true, err)
	}
	var runtimeError *RuntimeError
	if errors.As(err, &runtimeError) {
		return err
	}
	if errors.Is(err, driver.ErrBadConn) {
		return NewRuntimeError(databasev1.ErrorConnectionUnavailable, true, err)
	}
	if transaction && errors.Is(err, sql.ErrTxDone) {
		return NewRuntimeError(databasev1.ErrorTransactionLost, true, err)
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		class := postgresError.Code
		if len(class) >= 2 {
			class = class[:2]
		}
		switch class {
		case "08", "57", "58":
			return NewRuntimeError(databasev1.ErrorConnectionUnavailable, true, err)
		case "40":
			if transaction {
				return NewRuntimeError(databasev1.ErrorTransactionConflict, true, err)
			}
			return NewRuntimeError(databasev1.ErrorQueryFailed, true, err)
		case "53":
			return NewRuntimeError(databasev1.ErrorPoolExhausted, true, err)
		default:
			return NewRuntimeError(databasev1.ErrorQueryFailed, false, err)
		}
	}
	var mysqlError *mysql.MySQLError
	if errors.As(err, &mysqlError) {
		switch mysqlError.Number {
		case 1040, 1203:
			return NewRuntimeError(databasev1.ErrorPoolExhausted, true, err)
		case 1205, 1213:
			if transaction {
				return NewRuntimeError(databasev1.ErrorTransactionConflict, true, err)
			}
			return NewRuntimeError(databasev1.ErrorQueryFailed, true, err)
		case 1053, 2002, 2003, 2006, 2013:
			return NewRuntimeError(databasev1.ErrorConnectionUnavailable, true, err)
		default:
			return NewRuntimeError(databasev1.ErrorQueryFailed, false, err)
		}
	}
	var networkError net.Error
	if errors.As(err, &networkError) {
		return NewRuntimeError(databasev1.ErrorConnectionUnavailable, true, err)
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "connection refused") || strings.Contains(message, "connection reset") ||
		strings.Contains(message, "broken pipe") || strings.Contains(message, "server closed") {
		return NewRuntimeError(databasev1.ErrorConnectionUnavailable, true, err)
	}
	return NewRuntimeError(databasev1.ErrorQueryFailed, false, err)
}
