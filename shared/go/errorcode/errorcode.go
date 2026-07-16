// Package errorcode 定义 Backend Kernel 公共调用链使用的稳定错误码。
//
// 错误码是协议契约，不是日志文案：调用方可以按 code 分支，message 只用于诊断。
// 内核定义的 code 必须集中在此处；插件自定义 code 也必须遵守两段以上的小写
// 点分命名空间，避免与未来内核错误发生无声冲突。
package errorcode

import "regexp"

const (
	// 应用层错误：放在 contract.v1.Error，由调用方正常接收。
	CapabilityNotFound = "capability.not_found"
	CallCycleDetected  = "call.cycle_detected"
	CallDepthExceeded  = "call.depth_exceeded"
	HookAborted        = "hook.aborted"
	HostCallFailed     = "hostcall.failed"
	KernelServiceError = "kernel.service_error"
	PermissionDenied   = "permission.denied"
	PluginHandlerError = "plugin.handler_error"
	PluginInactive     = "plugin.inactive"
	PayloadTooLarge    = "resource.payload_too_large"
	MetadataTooLarge   = "resource.metadata_too_large"
	QueueFull          = "resource.queue_full"
	ConcurrencyLimited = "resource.concurrency_limited"

	// 传输层错误：表示请求没有得到可信的应用层 CallResult。
	RemoteInvalidResponse = "remote.invalid_response"
	RemoteInvokeFailed    = "remote.invoke_failed"
	RemoteStreamFailed    = "remote.stream_failed"
	WireInvalidRequest    = "wire.invalid_request"
	WireTargetMismatch    = "wire.target_mismatch"
)

var (
	codePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*(?:\.[a-z][a-z0-9_]*)+$`)
	kernelCodes = map[string]struct{}{
		CapabilityNotFound:    {},
		CallCycleDetected:     {},
		CallDepthExceeded:     {},
		HookAborted:           {},
		HostCallFailed:        {},
		KernelServiceError:    {},
		PermissionDenied:      {},
		PluginHandlerError:    {},
		PluginInactive:        {},
		PayloadTooLarge:       {},
		MetadataTooLarge:      {},
		QueueFull:             {},
		ConcurrencyLimited:    {},
		RemoteInvalidResponse: {},
		RemoteInvokeFailed:    {},
		RemoteStreamFailed:    {},
		WireInvalidRequest:    {},
		WireTargetMismatch:    {},
	}
)

// Valid 判断 code 是否符合公共契约的命名空间格式。
func Valid(code string) bool { return codePattern.MatchString(code) }

// KernelDefined 判断 code 是否由 Backend Kernel 规范定义。
func KernelDefined(code string) bool {
	_, ok := kernelCodes[code]
	return ok
}

// KernelCodes 返回内核错误码的稳定集合副本，供兼容性门禁使用。
func KernelCodes() []string {
	out := make([]string, 0, len(kernelCodes))
	for code := range kernelCodes {
		out = append(out, code)
	}
	return out
}
