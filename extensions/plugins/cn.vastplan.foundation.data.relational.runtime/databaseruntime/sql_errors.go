package databaseruntime

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"database/sql/driver"
	"errors"
	"net"
	"strings"
	"syscall"

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
	if classified, ok := classifyCredentialLeaseError(err); ok {
		return classified
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
	var certificateVerificationError *tls.CertificateVerificationError
	var unknownAuthority x509.UnknownAuthorityError
	var hostnameError x509.HostnameError
	var certificateInvalid x509.CertificateInvalidError
	if errors.As(err, &certificateVerificationError) || errors.As(err, &unknownAuthority) ||
		errors.As(err, &hostnameError) || errors.As(err, &certificateInvalid) {
		return NewRuntimeError(databasev1.ErrorTLSVerificationFailed, false, err)
	}
	var dnsError *net.DNSError
	if errors.As(err, &dnsError) {
		if dnsError.IsTimeout {
			return NewRuntimeError(databasev1.ErrorConnectionTimeout, true, err)
		}
		return NewRuntimeError(databasev1.ErrorNameResolutionFailed, true, err)
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return NewRuntimeError(databasev1.ErrorConnectionRefused, true, err)
	}
	if errors.Is(err, syscall.ENETUNREACH) || errors.Is(err, syscall.EHOSTUNREACH) {
		return NewRuntimeError(databasev1.ErrorConnectionUnavailable, true, err)
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "28P01":
			return NewRuntimeError(databasev1.ErrorAuthenticationFailed, false, err)
		case "3D000":
			return NewRuntimeError(databasev1.ErrorDatabaseNotFound, false, err)
		case "42501":
			return NewRuntimeError(databasev1.ErrorPermissionDenied, false, err)
		}
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
		case 1045:
			return NewRuntimeError(databasev1.ErrorAuthenticationFailed, false, err)
		case 1049:
			return NewRuntimeError(databasev1.ErrorDatabaseNotFound, false, err)
		case 1044:
			return NewRuntimeError(databasev1.ErrorPermissionDenied, false, err)
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
		if networkError.Timeout() {
			return NewRuntimeError(databasev1.ErrorConnectionTimeout, true, err)
		}
		return NewRuntimeError(databasev1.ErrorConnectionUnavailable, true, err)
	}
	message := strings.ToLower(err.Error())
	if containsAny(message, "password authentication failed", "access denied for user") {
		return NewRuntimeError(databasev1.ErrorAuthenticationFailed, false, err)
	}
	if containsAny(message, "unknown database", "database does not exist") {
		return NewRuntimeError(databasev1.ErrorDatabaseNotFound, false, err)
	}
	if containsAny(message, "certificate signed by unknown authority", "certificate is not valid for", "certificate verification failed",
		"server refused tls connection", "server does not support ssl", "tls handshake", "ssl handshake") {
		return NewRuntimeError(databasev1.ErrorTLSVerificationFailed, false, err)
	}
	if containsAny(message, "no such host", "temporary failure in name resolution") {
		return NewRuntimeError(databasev1.ErrorNameResolutionFailed, true, err)
	}
	if strings.Contains(message, "connection refused") {
		return NewRuntimeError(databasev1.ErrorConnectionRefused, true, err)
	}
	if containsAny(message, "i/o timeout", "connection timed out") {
		return NewRuntimeError(databasev1.ErrorConnectionTimeout, true, err)
	}
	if containsAny(message, "connection reset", "broken pipe", "server closed") {
		return NewRuntimeError(databasev1.ErrorConnectionUnavailable, true, err)
	}
	return NewRuntimeError(databasev1.ErrorQueryFailed, false, err)
}
