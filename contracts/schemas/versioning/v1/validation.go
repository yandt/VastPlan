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
		return validateParents(request.Stream, request.Parents, 0)
	case *GetVersionRequest:
		return ValidateVersionRef(request.Ref)
	case *ListHistoryRequest:
		if err := ValidateStreamKey(request.Stream); err != nil {
			return err
		}
		if request.Start != nil && request.Cursor != "" {
			return errors.New("历史请求不能同时指定 start 和 cursor")
		}
		if request.Start != nil && (ValidateVersionRef(*request.Start) != nil || request.Start.Stream != request.Stream) {
			return errors.New("历史起点必须属于同一 stream")
		}
		return nil
	case *GetHeadRequest:
		return validateHeadIdentity(request.Stream, request.Name)
	case *ListHeadsRequest:
		return validateReferencePage(request.Stream, request.Limit, request.Cursor)
	case *CreateHeadRequest:
		return validateReferenceTarget(request.Stream, request.Name, request.Target)
	case *MoveHeadRequest:
		if err := validateHeadIdentity(request.Stream, request.Name); err != nil {
			return err
		}
		if err := ValidateVersionRef(request.Target); err != nil || request.Target.Stream != request.Stream {
			return errors.New("Head 目标必须属于同一 stream")
		}
		return nil
	case *DeleteHeadRequest:
		if request.ExpectedRevision == 0 {
			return errors.New("删除 Head 必须提供 expectedRevision")
		}
		return validateHeadIdentity(request.Stream, request.Name)
	case *CreateTagRequest:
		return validateReferenceTarget(request.Stream, request.Name, request.Target)
	case *GetTagRequest:
		return validateHeadIdentity(request.Stream, request.Name)
	case *ListTagsRequest:
		return validateReferencePage(request.Stream, request.Limit, request.Cursor)
	case *CompareVersionsRequest:
		return validateVersionPair(request.Left, request.Right)
	case *IsAncestorRequest:
		return validateVersionPair(request.Ancestor, request.Descendant)
	case *FindCommonAncestorRequest:
		return validateVersionPair(request.Left, request.Right)
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
	case *ListHeadsResult:
		return validateHeadPage(*result)
	case *CreateHeadResult:
		return ValidateHead(result.Head)
	case *MoveHeadResult:
		return ValidateHead(result.Head)
	case *DeleteHeadResult:
		return ValidateHead(result.Previous)
	case *CreateTagResult:
		return ValidateTag(result.Tag)
	case *GetTagResult:
		return ValidateTag(result.Tag)
	case *ListTagsResult:
		return validateTagPage(*result)
	case *CompareVersionsResult:
		return ValidateComparisonResult(*result)
	case *IsAncestorResult:
		return ValidateIsAncestorResult(*result)
	case *FindCommonAncestorResult:
		return ValidateCommonAncestorResult(*result)
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
	if record.Protocol != Protocol || !validActorID(record.ActorID) || record.CreatedAt.IsZero() {
		return errors.New("VersionRecord 协议、actor 或时间无效")
	}
	if err := ValidateVersionRef(record.Ref); err != nil {
		return err
	}
	if err := validateParents(record.Ref.Stream, record.Parents, record.Ref.Sequence); err != nil {
		return err
	}
	digest, err := ContentDigest(record.Content)
	if err != nil || digest != record.Ref.ContentDigest {
		return errors.New("VersionRecord 内容摘要不匹配")
	}
	return nil
}

func ValidateProviderVersionCandidate(candidate *ProviderVersionCandidate) error {
	if candidate == nil || !sha256Pattern.MatchString(candidate.VersionID) || !validActorID(candidate.ActorID) {
		return errors.New("Provider version candidate actor 无效")
	}
	if err := ValidateStreamKey(candidate.Stream); err != nil {
		return err
	}
	canonical, err := CanonicalizeContent(candidate.Content)
	if err != nil {
		return err
	}
	candidate.Content = canonical
	return validateParents(candidate.Stream, candidate.Parents, 0)
}

func validActorID(actorID string) bool { return actorID != "" && len(actorID) <= 160 }

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

func ValidateTag(tag Tag) error {
	if tag.Protocol != Protocol || !validActorID(tag.ActorID) || tag.CreatedAt.IsZero() {
		return errors.New("Version Tag 协议、actor 或时间无效")
	}
	return validateReferenceTarget(tag.Stream, tag.Name, tag.Target)
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
		if len(previous.Parents) == 0 || previous.Parents[0] != current.Ref {
			return errors.New("Version history 未按父链降序返回")
		}
	}
	return nil
}

func validateParents(stream StreamKey, parents []VersionRef, childSequence uint64) error {
	if len(parents) > MaxParents {
		return errors.New("版本父节点超过限制")
	}
	seen := map[string]struct{}{}
	for _, parent := range parents {
		if err := ValidateVersionRef(parent); err != nil || parent.Stream != stream || (childSequence > 0 && parent.Sequence >= childSequence) {
			return errors.New("版本父节点无效")
		}
		if _, duplicate := seen[parent.VersionID]; duplicate {
			return errors.New("版本父节点重复")
		}
		seen[parent.VersionID] = struct{}{}
	}
	return nil
}

func validateReferenceTarget(stream StreamKey, name string, target VersionRef) error {
	if err := validateHeadIdentity(stream, name); err != nil {
		return err
	}
	if err := ValidateVersionRef(target); err != nil || target.Stream != stream {
		return errors.New("版本引用目标必须属于同一 stream")
	}
	return nil
}

func validateReferencePage(stream StreamKey, limit int, cursor string) error {
	if err := ValidateStreamKey(stream); err != nil || limit < 1 || limit > MaxRefsPage {
		return errors.New("版本引用分页请求无效")
	}
	if cursor != "" && !headPattern.MatchString(cursor) {
		return errors.New("版本引用 cursor 无效")
	}
	return nil
}

func validateVersionPair(left, right VersionRef) error {
	if err := ValidateVersionRef(left); err != nil {
		return err
	}
	if err := ValidateVersionRef(right); err != nil || left.Stream != right.Stream {
		return errors.New("两个版本必须属于同一 stream")
	}
	return nil
}

func validateProviderCreateTagRequest(request ProviderCreateTagRequest) error {
	if !validActorID(request.ActorID) {
		return errors.New("Provider Tag actor 无效")
	}
	return validateReferenceTarget(request.Stream, request.Name, request.Target)
}

func validateHeadPage(result ListHeadsResult) error {
	previous := ""
	for _, head := range result.Heads {
		if err := ValidateHead(head); err != nil || (previous != "" && head.Name <= previous) {
			return errors.New("Head 列表无效或未按名称升序")
		}
		previous = head.Name
	}
	return nil
}

func validateTagPage(result ListTagsResult) error {
	previous := ""
	for _, tag := range result.Tags {
		if err := ValidateTag(tag); err != nil || (previous != "" && tag.Name <= previous) {
			return errors.New("Tag 列表无效或未按名称升序")
		}
		previous = tag.Name
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
