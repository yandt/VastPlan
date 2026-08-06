package databaseruntime

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"syscall"

	mysql "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/credentiallease"
)

// classifyProviderValidationError keeps deployment policy failures separate
// from ordinary malformed Provider options. Callers branch only on the stable
// code; the original error remains inside the trusted Runtime process.
func classifyProviderValidationError(err error) (string, bool) {
	if errors.Is(err, errTLSPolicyForbidden) {
		return databasev1.ErrorTLSPolicyForbidden, false
	}
	return databasev1.ErrorInvalidRequest, false
}

func runtimeSafeMessage(code string) string {
	switch code {
	case databasev1.ErrorInvalidRequest, databasev1.ErrorProviderNotFound, databasev1.ErrorUnsupported:
		return "数据库连接配置无效"
	case databasev1.ErrorConnectionNotFound:
		return "数据库连接定义不存在"
	case databasev1.ErrorCredentialUnavailable:
		return "数据库密码不可用或已经失效"
	case databasev1.ErrorCredentialServiceUnavailable:
		return "数据库凭证服务暂时不可用"
	case databasev1.ErrorTLSPolicyForbidden:
		return "当前部署策略不允许关闭数据库传输加密校验"
	case databasev1.ErrorNameResolutionFailed:
		return "数据库地址无法解析"
	case databasev1.ErrorConnectionRefused:
		return "数据库服务器拒绝了连接"
	case databasev1.ErrorConnectionTimeout, databasev1.ErrorDeadlineExceeded:
		return "连接数据库超时"
	case databasev1.ErrorTLSVerificationFailed:
		return "数据库传输加密或证书校验失败"
	case databasev1.ErrorAuthenticationFailed:
		return "数据库用户名或密码验证失败"
	case databasev1.ErrorDatabaseNotFound:
		return "指定的数据库不存在"
	case databasev1.ErrorPermissionDenied:
		return "数据库账户没有所需权限"
	case databasev1.ErrorPoolExhausted:
		return "数据库连接资源暂时不足"
	default:
		return "数据库运行时操作失败"
	}
}

// RuntimeSafeMessage returns the localized, value-free message associated
// with a stable Database Runtime error code. It is safe to cross plugin and
// browser boundaries; raw driver errors remain inside the Runtime process.
func RuntimeSafeMessage(code string) string { return runtimeSafeMessage(code) }

// logRuntimeDiagnostic records a useful but non-secret technical conclusion.
// It deliberately never logs endpoint, username, database, SQL, CredentialRef
// or the raw driver message, which may embed those values.
func logRuntimeDiagnostic(call *contractv1.CallContext, operation, providerID, stage string, err error) {
	code, retryable := ErrorDetails(err)
	diagnostic, driverCode := diagnosticEvidence(err)
	attributes := []any{
		"component", "database-runtime",
		"operation", operation,
		"stage", stage,
		"provider", providerID,
		"error_code", code,
		"retryable", retryable,
		"diagnostic", diagnostic,
		"cause_type", fmt.Sprintf("%T", rootDiagnosticCause(err)),
	}
	if driverCode != "" {
		attributes = append(attributes, "driver_code", driverCode)
	}
	if call != nil && call.GetTrace() != nil {
		attributes = append(attributes, "trace_id", call.GetTrace().GetTraceId(), "span_id", call.GetTrace().GetSpanId())
	}
	slog.Warn("database runtime operation failed", attributes...)
}

// LogRuntimeDiagnostic exposes the same bounded diagnostic path to internal
// Database Runtime modules such as Platform Control Bootstrap.
func LogRuntimeDiagnostic(call *contractv1.CallContext, operation, providerID, stage string, err error) {
	logRuntimeDiagnostic(call, operation, providerID, stage, err)
}

func diagnosticEvidence(err error) (diagnostic, driverCode string) {
	if code, _, ok := credentiallease.FailureDetails(err); ok {
		switch code {
		case credentiallease.ErrorReferenceUnavailable:
			return "credential_reference_unavailable", ""
		case credentiallease.ErrorMaterialUnavailable:
			return "credential_material_unavailable", ""
		case credentiallease.ErrorChanged:
			return "credential_changed", ""
		case credentiallease.ErrorDenied:
			return "credential_lease_denied", ""
		default:
			return "credential_service_unavailable", ""
		}
	}
	root := rootDiagnosticCause(err)
	var postgresError *pgconn.PgError
	if errors.As(root, &postgresError) {
		return "postgresql_server_rejected", postgresError.Code
	}
	var mysqlError *mysql.MySQLError
	if errors.As(root, &mysqlError) {
		return "mysql_server_rejected", fmt.Sprintf("%d", mysqlError.Number)
	}
	var dnsError *net.DNSError
	if errors.As(root, &dnsError) {
		if dnsError.IsTimeout {
			return "dns_timeout", ""
		}
		return "dns_resolution_failed", ""
	}
	var certificateVerificationError *tls.CertificateVerificationError
	if errors.As(root, &certificateVerificationError) {
		return "tls_certificate_verification_failed", ""
	}
	var unknownAuthority x509.UnknownAuthorityError
	var hostnameError x509.HostnameError
	var certificateInvalid x509.CertificateInvalidError
	if errors.As(root, &unknownAuthority) || errors.As(root, &hostnameError) || errors.As(root, &certificateInvalid) {
		return "tls_certificate_verification_failed", ""
	}
	if errors.Is(root, errTLSPolicyForbidden) {
		return "tls_policy_forbidden", ""
	}
	if errors.Is(root, context.DeadlineExceeded) {
		return "deadline_exceeded", ""
	}
	if errors.Is(root, syscall.ECONNREFUSED) {
		return "connection_refused", ""
	}
	if errors.Is(root, syscall.ENETUNREACH) || errors.Is(root, syscall.EHOSTUNREACH) {
		return "network_unreachable", ""
	}
	var networkError net.Error
	if errors.As(root, &networkError) && networkError.Timeout() {
		return "network_timeout", ""
	}
	return "unclassified", ""
}

func rootDiagnosticCause(err error) error {
	var runtimeError *RuntimeError
	if errors.As(err, &runtimeError) && runtimeError.Err != nil {
		return runtimeError.Err
	}
	return err
}

func containsAny(value string, fragments ...string) bool {
	for _, fragment := range fragments {
		if strings.Contains(value, fragment) {
			return true
		}
	}
	return false
}
