package versioncontentv1

import (
	"errors"
	"regexp"

	versioningv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versioning/v1"
	versionresourcev1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionresource/v1"
	stagingv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionstaging/v1"
)

var (
	operationIDPattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{7,159}$`)
	sessionIDPattern    = regexp.MustCompile(`^ws_[A-Za-z0-9_-]{16,96}$`)
	protectionIDPattern = regexp.MustCompile(`^vcr_[A-Za-z0-9_-]{16,96}$`)
	digestPattern       = regexp.MustCompile(`^[a-f0-9]{64}$`)
	uploadIDPattern     = regexp.MustCompile(`^stg_[A-Za-z0-9_-]{16,96}$`)
)

func ValidatePrepareRequest(request PrepareRequest) error {
	if !operationIDPattern.MatchString(request.OperationID) || !sessionIDPattern.MatchString(request.SessionID) || request.ExpectedSessionRevision == 0 || !digestPattern.MatchString(request.EnvironmentDigest) || !digestPattern.MatchString(request.ManifestDigest) {
		return errors.New("版本内容保护身份无效")
	}
	if versionresourcev1.ValidateResourceKey(request.Resource) != nil || versioningv1.ValidateStreamKey(request.Stream) != nil || len(request.Entries) > versionresourcev1.MaxFileEntries {
		return errors.New("版本内容保护资源或条目数量无效")
	}
	return validateEntries(request.Entries, true)
}

func ValidateStatusRequest(request StatusRequest) error {
	if !protectionIDPattern.MatchString(request.ProtectionID) {
		return errors.New("版本内容保护 ID 无效")
	}
	return nil
}

func ValidateConfirmRequest(request ConfirmRequest) error {
	if ValidateStatusRequest(StatusRequest{ProtectionID: request.ProtectionID}) != nil || request.ExpectedRevision == 0 || versioningv1.ValidateVersionRef(request.Version) != nil {
		return errors.New("确认版本内容保护请求无效")
	}
	return nil
}

func ValidateAbortRequest(request AbortRequest) error {
	if ValidateStatusRequest(StatusRequest{ProtectionID: request.ProtectionID}) != nil || request.ExpectedRevision == 0 {
		return errors.New("终止版本内容保护请求无效")
	}
	return nil
}

func ValidateProtection(value Protection) error {
	if value.Protocol != Protocol || !protectionIDPattern.MatchString(value.ID) || !operationIDPattern.MatchString(value.OperationID) || !digestPattern.MatchString(value.EnvironmentDigest) || !digestPattern.MatchString(value.ManifestDigest) || versionresourcev1.ValidateResourceKey(value.Resource) != nil || versioningv1.ValidateStreamKey(value.Stream) != nil {
		return errors.New("版本内容保护状态身份无效")
	}
	if value.Revision == 0 || value.CreatedAt.IsZero() || value.UpdatedAt.Before(value.CreatedAt) || validateEntries(value.Entries, false) != nil {
		return errors.New("版本内容保护状态无效")
	}
	switch value.State {
	case StatePrepared:
		if value.Version != nil || value.ExpiresAt == nil || !value.ExpiresAt.After(value.CreatedAt) {
			return errors.New("Prepared 内容保护生命周期无效")
		}
	case StateConfirmed:
		if value.Version == nil || value.ExpiresAt != nil || versioningv1.ValidateVersionRef(*value.Version) != nil || value.Version.Stream != value.Stream || value.Version.ContentDigest != value.ManifestDigest {
			return errors.New("Confirmed 内容保护版本引用无效")
		}
	case StateAborted, StateExpired:
		if value.Version != nil || value.ExpiresAt != nil {
			return errors.New("终态内容保护不得持有版本或过期时间")
		}
	default:
		return errors.New("版本内容保护状态枚举无效")
	}
	return nil
}

func validateEntries(entries []ContentEntry, allowUploadID bool) error {
	previous := ""
	for _, entry := range entries {
		if versionresourcev1.ValidateFilePath(entry.Path) != nil || entry.Path <= previous || !digestPattern.MatchString(entry.Digest) || entry.Size < 0 || entry.Size > stagingv1.MaximumDeclaredFileBytes || versionresourcev1.ValidateMediaType(entry.MediaType) != nil {
			return errors.New("版本内容保护条目无效或未按 path 严格排序")
		}
		if entry.UploadID != "" && (!allowUploadID || !uploadIDPattern.MatchString(entry.UploadID)) {
			return errors.New("版本内容保护 Upload ID 无效")
		}
		previous = entry.Path
	}
	return nil
}
