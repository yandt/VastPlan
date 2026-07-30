package versioningv1

import (
	"errors"
	"fmt"
	"regexp"
)

var (
	namespacePattern   = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)+$`)
	resourcePattern    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,159}$`)
	headPattern        = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,63}$`)
	idempotencyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{7,159}$`)
	sha256Pattern      = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

var knownErrors = map[string]struct{}{
	ErrorInvalidRequest: {}, ErrorProviderNotFound: {}, ErrorProviderUnavailable: {},
	ErrorNotFound: {}, ErrorConflict: {}, ErrorDigestMismatch: {}, ErrorCorrupted: {},
	ErrorLimitExceeded: {}, ErrorUnsupported: {},
}

func validateRequest(target any) error {
	switch request := target.(type) {
	case *ProviderListRequest:
		return nil
	case *PutVersionRequest:
		if err := ValidateStreamKey(request.Stream); err != nil || !idempotencyPattern.MatchString(request.IdempotencyKey) {
			return errors.New("版本 stream 或 idempotencyKey 无效")
		}
		canonical, err := CanonicalizeContent(request.Content)
		if err != nil {
			return err
		}
		request.Content = canonical
		if request.Parent != nil {
			if err := ValidateVersionRef(*request.Parent); err != nil || request.Parent.Stream != request.Stream {
				return errors.New("父版本必须属于同一 stream")
			}
		}
		return nil
	case *GetVersionRequest:
		return ValidateVersionRef(request.Ref)
	case *ListHistoryRequest:
		if err := ValidateStreamKey(request.Stream); err != nil {
			return err
		}
		if request.Start != nil && (ValidateVersionRef(*request.Start) != nil || request.Start.Stream != request.Stream) {
			return errors.New("历史起点必须属于同一 stream")
		}
		return nil
	case *GetHeadRequest:
		return validateHeadIdentity(request.Stream, request.Name)
	case *MoveHeadRequest:
		if err := validateHeadIdentity(request.Stream, request.Name); err != nil {
			return err
		}
		if err := ValidateVersionRef(request.Target); err != nil || request.Target.Stream != request.Stream {
			return errors.New("Head 目标必须属于同一 stream")
		}
		return nil
	default:
		return errors.New("Version Ledger 请求类型无效")
	}
}

func validateResult(target any) error {
	switch result := target.(type) {
	case *ProviderListResult:
		seen := map[string]struct{}{}
		for _, provider := range result.Providers {
			if err := ValidateProviderDescriptor(provider); err != nil {
				return err
			}
			if _, duplicate := seen[provider.ID]; duplicate {
				return fmt.Errorf("Version Provider %q 重复", provider.ID)
			}
			seen[provider.ID] = struct{}{}
		}
		return nil
	case *PutVersionResult:
		return ValidateVersionRecord(result.Version)
	case *GetVersionResult:
		return ValidateVersionRecord(result.Version)
	case *ListHistoryResult:
		return ValidateHistory(result.Versions)
	case *GetHeadResult:
		return ValidateHead(result.Head)
	case *MoveHeadResult:
		return ValidateHead(result.Head)
	default:
		return errors.New("Version Ledger 结果类型无效")
	}
}

func ValidateStreamKey(key StreamKey) error {
	if len(key.Namespace) > 128 || !namespacePattern.MatchString(key.Namespace) || !resourcePattern.MatchString(key.StreamID) {
		return errors.New("Version Ledger stream 标识无效")
	}
	return nil
}

func ValidateVersionRef(ref VersionRef) error {
	if err := ValidateStreamKey(ref.Stream); err != nil || ref.Sequence == 0 || !sha256Pattern.MatchString(ref.VersionID) || !sha256Pattern.MatchString(ref.ContentDigest) {
		return errors.New("VersionRef 无效")
	}
	return nil
}

func ValidateVersionRecord(record VersionRecord) error {
	if record.Protocol != Protocol || record.ActorID == "" || len(record.ActorID) > 160 || record.CreatedAt.IsZero() {
		return errors.New("VersionRecord 协议、actor 或时间无效")
	}
	if err := ValidateVersionRef(record.Ref); err != nil {
		return err
	}
	if record.Parent != nil {
		if err := ValidateVersionRef(*record.Parent); err != nil || record.Parent.Stream != record.Ref.Stream || record.Parent.Sequence >= record.Ref.Sequence {
			return errors.New("VersionRecord 父版本无效")
		}
	}
	digest, err := ContentDigest(record.Content)
	if err != nil || digest != record.Ref.ContentDigest {
		return errors.New("VersionRecord 内容摘要不匹配")
	}
	return nil
}

func ValidateHead(head Head) error {
	if head.Protocol != Protocol || head.Revision == 0 || head.UpdatedAt.IsZero() {
		return errors.New("Version Head 协议、revision 或时间无效")
	}
	if err := validateHeadIdentity(head.Stream, head.Name); err != nil {
		return err
	}
	if err := ValidateVersionRef(head.Target); err != nil || head.Target.Stream != head.Stream {
		return errors.New("Version Head 目标无效")
	}
	return nil
}

func ValidateHistory(versions []VersionRecord) error {
	for index := range versions {
		if err := ValidateVersionRecord(versions[index]); err != nil {
			return err
		}
		if index == 0 {
			continue
		}
		previous := versions[index-1]
		current := versions[index]
		if previous.Parent == nil || *previous.Parent != current.Ref {
			return errors.New("Version history 未按父链降序返回")
		}
	}
	return nil
}

func KnownErrorCode(code string) bool { _, ok := knownErrors[code]; return ok }

func validateHeadIdentity(stream StreamKey, name string) error {
	if err := ValidateStreamKey(stream); err != nil || !headPattern.MatchString(name) {
		return errors.New("Version Head 标识无效")
	}
	return nil
}
