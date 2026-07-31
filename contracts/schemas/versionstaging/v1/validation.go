package versionstagingv1

import (
	"errors"
	"regexp"

	versionresourcev1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionresource/v1"
)

var (
	uploadIDPattern = regexp.MustCompile(`^stg_[A-Za-z0-9_-]{16,96}$`)
	sessionPattern  = regexp.MustCompile(`^ws_[A-Za-z0-9_-]{16,96}$`)
	digestPattern   = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

func ValidateBeginUploadRequest(request BeginUploadRequest) error {
	if !sessionPattern.MatchString(request.SessionID) || request.ExpectedSessionRevision == 0 || !digestPattern.MatchString(request.EnvironmentDigest) || versionresourcev1.ValidateResourceKey(request.Resource) != nil {
		return errors.New("开始内容暂存请求身份无效")
	}
	if versionresourcev1.ValidateFilePath(request.Path) != nil || versionresourcev1.ValidateMediaType(request.MediaType) != nil || !digestPattern.MatchString(request.ExpectedDigest) || request.ExpectedSize < 0 || request.ExpectedSize > MaximumDeclaredFileBytes {
		return errors.New("开始内容暂存请求文件声明无效")
	}
	if request.LeaseSeconds < MinimumLeaseSeconds || request.LeaseSeconds > MaximumLeaseSeconds {
		return errors.New("开始内容暂存请求 Lease 无效")
	}
	return nil
}

func ValidateUploadLease(upload UploadLease) error {
	if upload.Protocol != Protocol || !uploadIDPattern.MatchString(upload.ID) || !sessionPattern.MatchString(upload.SessionID) || !digestPattern.MatchString(upload.EnvironmentDigest) || versionresourcev1.ValidateResourceKey(upload.Resource) != nil {
		return errors.New("内容暂存 Lease 身份无效")
	}
	if versionresourcev1.ValidateFilePath(upload.Path) != nil || versionresourcev1.ValidateMediaType(upload.MediaType) != nil || !digestPattern.MatchString(upload.ExpectedDigest) || upload.ExpectedSize < 0 || upload.ExpectedSize > MaximumDeclaredFileBytes || upload.ReceivedSize < 0 || upload.ReceivedSize > upload.ExpectedSize {
		return errors.New("内容暂存 Lease 文件状态无效")
	}
	if !validState(upload.State) || upload.Revision == 0 || upload.CreatedAt.IsZero() || upload.UpdatedAt.Before(upload.CreatedAt) || !upload.LeaseExpiresAt.After(upload.CreatedAt) {
		return errors.New("内容暂存 Lease 生命周期无效")
	}
	if (upload.State == StateVerifying || upload.State == StateReady) && upload.ReceivedSize != upload.ExpectedSize {
		return errors.New("内容暂存校验状态与接收大小不一致")
	}
	return nil
}

func ValidateUploadStatusRequest(request UploadStatusRequest) error {
	if !uploadIDPattern.MatchString(request.UploadID) {
		return errors.New("内容暂存查询请求无效")
	}
	return nil
}

func ValidateUploadRevisionRequest(request UploadRevisionRequest) error {
	if !uploadIDPattern.MatchString(request.UploadID) || request.ExpectedRevision == 0 {
		return errors.New("内容暂存 CAS 请求无效")
	}
	return nil
}

func ValidateRenewUploadRequest(request RenewUploadRequest) error {
	if ValidateUploadRevisionRequest(UploadRevisionRequest{UploadID: request.UploadID, ExpectedRevision: request.ExpectedRevision}) != nil || request.LeaseSeconds < MinimumLeaseSeconds || request.LeaseSeconds > MaximumLeaseSeconds {
		return errors.New("内容暂存续租请求无效")
	}
	return nil
}

func ValidateContentDescriptor(content ContentDescriptor) error {
	if !digestPattern.MatchString(content.Digest) || content.Size < 0 || content.Size > MaximumDeclaredFileBytes || versionresourcev1.ValidateMediaType(content.MediaType) != nil {
		return errors.New("内容暂存完成描述无效")
	}
	return nil
}

func ValidateUploadStatusResult(result UploadStatusResult) error {
	if err := ValidateUploadLease(result.Upload); err != nil {
		return err
	}
	switch result.Upload.State {
	case StateReady:
		if result.Content == nil || result.FailureCode != "" || ValidateContentDescriptor(*result.Content) != nil || result.Content.Digest != result.Upload.ExpectedDigest || result.Content.Size != result.Upload.ExpectedSize || result.Content.MediaType != result.Upload.MediaType {
			return errors.New("已就绪暂存内容与 Lease 不匹配")
		}
	case StateRejected:
		if result.Content != nil || !validFailureCode(result.FailureCode) {
			return errors.New("被拒绝暂存内容缺少稳定失败码")
		}
	default:
		if result.Content != nil || result.FailureCode != "" {
			return errors.New("未完成暂存内容不得返回内容或失败结论")
		}
	}
	return nil
}

func validState(value string) bool {
	switch value {
	case StatePending, StateUploading, StateVerifying, StateReady, StateRejected, StateAborted, StateExpired:
		return true
	default:
		return false
	}
}

func validFailureCode(value string) bool {
	return value == FailureDigestMismatch || value == FailureSizeMismatch || value == FailureAdmissionRejected
}
