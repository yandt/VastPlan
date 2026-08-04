package credentiallease

import "errors"

const (
	ErrorInvalid              = "credential.material_lease.invalid"
	ErrorDenied               = "credential.material_lease.denied"
	ErrorReferenceUnavailable = "credential.material_lease.reference_unavailable"
	ErrorMaterialUnavailable  = "credential.material_lease.material_unavailable"
	ErrorChanged              = "credential.material_lease.changed"
	ErrorServiceUnavailable   = "credential.material_lease.service_unavailable"
)

var failureCodes = map[string]struct{}{
	ErrorInvalid: {}, ErrorDenied: {}, ErrorReferenceUnavailable: {},
	ErrorMaterialUnavailable: {}, ErrorChanged: {}, ErrorServiceUnavailable: {},
}

// Failure is the single stable error shape used by the credential custodian,
// Kernel broker and trusted runtime SDK. The wrapped error is trusted-process
// diagnostic detail and must never be copied into a browser response.
type Failure struct {
	Code      string
	Retryable bool
	Err       error
}

func (e *Failure) Error() string {
	if e == nil || e.Err == nil {
		return "material lease failed"
	}
	return e.Err.Error()
}

func (e *Failure) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func NewFailure(code string, retryable bool, err error) error {
	if !KnownFailureCode(code) {
		code, retryable = ErrorServiceUnavailable, true
	}
	if err == nil {
		err = errors.New(SafeFailureMessage(code))
	}
	return &Failure{Code: code, Retryable: retryable, Err: err}
}

func FailureDetails(err error) (code string, retryable bool, ok bool) {
	var failure *Failure
	if !errors.As(err, &failure) || !KnownFailureCode(failure.Code) {
		return "", false, false
	}
	return failure.Code, failure.Retryable, true
}

func KnownFailureCode(code string) bool {
	_, ok := failureCodes[code]
	return ok
}

func SafeFailureMessage(code string) string {
	switch code {
	case ErrorInvalid:
		return "Material Lease 请求或响应无效"
	case ErrorDenied:
		return "Material Lease 请求未获授权"
	case ErrorReferenceUnavailable:
		return "托管凭证引用不存在、未激活或已经失效"
	case ErrorMaterialUnavailable:
		return "托管凭证材料无法解密或与当前密钥不兼容"
	case ErrorChanged:
		return "托管凭证在签发期间发生变化"
	default:
		return "凭证材料服务暂时不可用"
	}
}
