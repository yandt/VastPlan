package main

import (
	"errors"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
)

type databaseDiagnostic struct {
	platformCode string
	message      string
}

var databaseDiagnostics = map[string]databaseDiagnostic{
	databasev1.ErrorInvalidRequest:        {"platform.database.invalid", "数据库连接参数不符合当前数据库类型要求"},
	databasev1.ErrorProviderNotFound:      {"platform.database.invalid", "当前数据库类型不可用"},
	databasev1.ErrorUnsupported:           {"platform.database.invalid", "当前数据库类型不支持该连接方式"},
	databasev1.ErrorTLSPolicyForbidden:    {"platform.database.tls_policy_forbidden", "当前部署策略不允许关闭数据库传输加密校验"},
	databasev1.ErrorNameResolutionFailed:  {"platform.database.name_resolution_failed", "数据库地址无法解析"},
	databasev1.ErrorConnectionRefused:     {"platform.database.connection_refused", "数据库服务器拒绝了连接"},
	databasev1.ErrorConnectionTimeout:     {"platform.database.connection_timeout", "连接数据库超时"},
	databasev1.ErrorDeadlineExceeded:      {"platform.database.connection_timeout", "连接数据库超时"},
	databasev1.ErrorTLSVerificationFailed: {"platform.database.tls_verification_failed", "数据库传输加密或证书校验失败"},
	databasev1.ErrorAuthenticationFailed:  {"platform.database.authentication_failed", "数据库用户名或密码验证失败"},
	databasev1.ErrorDatabaseNotFound:      {"platform.database.database_not_found", "指定的数据库不存在"},
	databasev1.ErrorPermissionDenied:      {"platform.database.permission_denied", "数据库账户没有所需权限"},
	databasev1.ErrorPoolExhausted:         {"platform.database.pool_exhausted", "数据库连接资源暂时不足"},
	databasev1.ErrorConnectionUnavailable: {"platform.database.connection_unavailable", "数据库连接暂时不可用"},
}

// databaseTestError exposes a stable diagnosis, never the Runtime's raw
// provider message. That message can contain endpoint, account or TLS details
// and remains available only as a sanitized Runtime log event.
func databaseTestError(err error) (*contractv1.CallResult, []byte, error) {
	var runtimeErr *runtimeCallError
	if !errors.As(err, &runtimeErr) {
		// Credential lifecycle and local persistence failures are not database
		// diagnoses and retain their existing failure semantics.
		return nil, nil, err
	}
	diagnostic, ok := databaseDiagnostics[runtimeErr.code]
	if !ok {
		diagnostic = databaseDiagnostic{"platform.database.runtime_unavailable", "数据库运行时暂时无法完成连接测试"}
	}
	return &contractv1.CallResult{
		Status: contractv1.CallResult_STATUS_ERROR,
		Error: &contractv1.Error{
			Code: diagnostic.platformCode, Message: diagnostic.message, Retryable: runtimeErr.retryable,
		},
	}, nil, nil
}
