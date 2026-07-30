package versionworkspacev1

import (
	"errors"
	"regexp"
	"sort"
	"strings"

	versioningv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versioning/v1"
	versionresourcev1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionresource/v1"
)

var (
	identityPattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,159}$`)
	sessionPattern   = regexp.MustCompile(`^ws_[A-Za-z0-9_-]{16,96}$`)
	headPattern      = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,63}$`)
	digestPattern    = regexp.MustCompile(`^[a-f0-9]{64}$`)
	adapterPattern   = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)+$`)
	operationPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{7,159}$`)
)

func ValidateOpenRequest(request OpenRequest) error {
	if !identityPattern.MatchString(request.EnvironmentID) || versionresourcev1.ValidateResourceKey(request.Resource) != nil || request.LeaseSeconds < 0 || request.LeaseSeconds > 86400 {
		return errors.New("打开版本工作区请求无效")
	}
	if request.RequestedMode != "" && !validMode(request.RequestedMode) || request.BaseHead != "" && !headPattern.MatchString(request.BaseHead) || request.TargetHead != "" && !headPattern.MatchString(request.TargetHead) {
		return errors.New("版本工作区模式或 Head 无效")
	}
	if request.BaseRef != nil && request.BaseHead != "" {
		return errors.New("版本工作区不能同时指定 BaseRef 和 BaseHead")
	}
	if request.BaseRef != nil && versioningv1.ValidateVersionRef(*request.BaseRef) != nil {
		return errors.New("版本工作区基线引用无效")
	}
	if request.ReadOnly && request.TargetHead != "" {
		return errors.New("只读工作区不得声明待更新 Head")
	}
	return nil
}

func ValidateSession(session Session) error {
	if session.Protocol != Protocol || !sessionPattern.MatchString(session.ID) || !identityPattern.MatchString(session.EnvironmentID) || !digestPattern.MatchString(session.EnvironmentDigest) || versionresourcev1.ValidateResourceKey(session.Resource) != nil {
		return errors.New("版本工作区 Session 身份无效")
	}
	if !validMode(session.Mode) || versioningv1.ValidateStreamKey(versioningv1.StreamKey{Namespace: session.Namespace, StreamID: session.Resource.ID}) != nil || !adapterPattern.MatchString(session.Adapter) || !validState(session.State) || session.Revision == 0 || session.CreatedAt.IsZero() || !session.LeaseExpiresAt.After(session.CreatedAt) {
		return errors.New("版本工作区 Session 状态无效")
	}
	if session.BaseRef != nil && (versioningv1.ValidateVersionRef(*session.BaseRef) != nil || session.BaseRef.Stream != (versioningv1.StreamKey{Namespace: session.Namespace, StreamID: session.Resource.ID})) || session.BaseHead != "" && !headPattern.MatchString(session.BaseHead) || session.TargetHead != "" && !headPattern.MatchString(session.TargetHead) {
		return errors.New("版本工作区 Session 基线或 Head 无效")
	}
	if session.BaseHead != "" && session.BaseRef == nil {
		return errors.New("版本工作区 Session 的 BaseHead 未解析为精确引用")
	}
	if session.TargetHead == "" && session.HeadRevision != 0 || session.ReadOnly && session.TargetHead != "" {
		return errors.New("版本工作区 Session Head 状态无效")
	}
	return nil
}

func ValidateSnapshotResult(result SnapshotResult, maxBytes int64) error {
	if err := ValidateSession(result.Session); err != nil || !digestPattern.MatchString(result.Digest) {
		return errors.New("版本工作区快照结果无效")
	}
	digest, err := versionresourcev1.SnapshotDigest(result.Snapshot, maxBytes)
	if err != nil || digest != result.Digest {
		return errors.New("版本工作区快照摘要不匹配")
	}
	return nil
}

func ValidateChangesResult(result ChangesResult) error {
	if err := ValidateSession(result.Session); err != nil {
		return err
	}
	return validateChangeDetails(result.ChangedPaths, result.Summary, result.Dirty)
}

func ValidateCommitRequest(request CommitRequest) error {
	if !sessionPattern.MatchString(request.SessionID) || request.ExpectedRevision == 0 || !operationPattern.MatchString(request.OperationID) || len(request.Message) > 1024 || len(request.Labels) > 16 {
		return errors.New("提交版本工作区请求无效")
	}
	for key, value := range request.Labels {
		if strings.TrimSpace(key) == "" || len(key) > 64 || len(value) > 256 {
			return errors.New("版本工作区提交标签无效")
		}
	}
	return nil
}

func ValidateCommittedRequest(request CommittedRequest) error {
	if !identityPattern.MatchString(request.EnvironmentID) || !digestPattern.MatchString(request.EnvironmentDigest) || versionresourcev1.ValidateResourceKey(request.Resource) != nil || versioningv1.ValidateVersionRef(request.Ref) != nil {
		return errors.New("读取已提交版本请求无效")
	}
	if request.RequestedMode != "" && !validMode(request.RequestedMode) {
		return errors.New("读取已提交版本模式无效")
	}
	return nil
}

func ValidateCompareCommittedRequest(request CompareCommittedRequest) error {
	if !identityPattern.MatchString(request.EnvironmentID) || !digestPattern.MatchString(request.EnvironmentDigest) || versionresourcev1.ValidateResourceKey(request.Resource) != nil || versioningv1.ValidateVersionRef(request.Left) != nil || versioningv1.ValidateVersionRef(request.Right) != nil || request.Left.Stream != request.Right.Stream {
		return errors.New("比较已提交版本请求无效")
	}
	if request.RequestedMode != "" && !validMode(request.RequestedMode) {
		return errors.New("比较已提交版本模式无效")
	}
	return nil
}

func ValidateResourceResolution(resolution ResourceResolution) error {
	if !identityPattern.MatchString(resolution.EnvironmentID) || !digestPattern.MatchString(resolution.EnvironmentDigest) || versionresourcev1.ValidateResourceKey(resolution.Resource) != nil || !adapterPattern.MatchString(resolution.Adapter) || !validMode(resolution.Mode) {
		return errors.New("Version Workspace 资源解析结果无效")
	}
	stream := versioningv1.StreamKey{Namespace: resolution.Namespace, StreamID: resolution.Resource.ID}
	return versioningv1.ValidateStreamKey(stream)
}

func ValidateCommittedSnapshotResult(result CommittedSnapshotResult, maxBytes int64) error {
	if ValidateResourceResolution(result.Resolution) != nil || versioningv1.ValidateVersionRecord(result.Version) != nil || !digestPattern.MatchString(result.Digest) {
		return errors.New("Version Workspace 已提交快照结果无效")
	}
	stream := versioningv1.StreamKey{Namespace: result.Resolution.Namespace, StreamID: result.Resolution.Resource.ID}
	if result.Version.Ref.Stream != stream || result.Version.Ref.ContentDigest != result.Digest {
		return errors.New("Version Workspace 已提交版本与资源不匹配")
	}
	digest, err := versionresourcev1.SnapshotDigest(result.Snapshot, maxBytes)
	if err != nil || digest != result.Digest {
		return errors.New("Version Workspace 已提交快照摘要不匹配")
	}
	return nil
}

func ValidateCompareCommittedResult(result CompareCommittedResult) error {
	if ValidateResourceResolution(result.Resolution) != nil || versioningv1.ValidateVersionRef(result.Left) != nil || versioningv1.ValidateVersionRef(result.Right) != nil || !digestPattern.MatchString(result.LeftDigest) || !digestPattern.MatchString(result.RightDigest) {
		return errors.New("Version Workspace 已提交版本比较结果无效")
	}
	stream := versioningv1.StreamKey{Namespace: result.Resolution.Namespace, StreamID: result.Resolution.Resource.ID}
	if result.Left.Stream != stream || result.Right.Stream != stream || result.LeftDigest != result.Left.ContentDigest || result.RightDigest != result.Right.ContentDigest || result.Dirty != (result.LeftDigest != result.RightDigest) {
		return errors.New("Version Workspace 已提交版本比较身份不匹配")
	}
	if !result.DiffAvailable {
		if len(result.ChangedPaths) != 0 || result.Summary != (ChangeSummary{}) {
			return errors.New("Version Workspace 不可用 diff 不得返回详细变化")
		}
		return nil
	}
	return validateChangeDetails(result.ChangedPaths, result.Summary, result.Dirty)
}

func validateChangeDetails(paths []string, summary ChangeSummary, dirty bool) error {
	if len(paths) > MaxChangedPaths || summary.Total != summary.Added+summary.Modified+summary.Removed || summary.Total < 0 {
		return errors.New("版本工作区变更结果无效")
	}
	copyPaths := append([]string(nil), paths...)
	if !sort.StringsAreSorted(copyPaths) || dirty != (summary.Total > 0) {
		return errors.New("版本工作区变更列表未排序或状态不一致")
	}
	for index, value := range copyPaths {
		if strings.TrimSpace(value) == "" || index > 0 && copyPaths[index-1] == value {
			return errors.New("版本工作区变更路径无效或重复")
		}
	}
	return nil
}

func validMode(value string) bool {
	return value == versionresourcev1.ModeSnapshot || value == versionresourcev1.ModeOverlay || value == versionresourcev1.ModeGit
}

func validState(value string) bool {
	switch value {
	case StateClean, StateDirty, StateCommitting, StateCommitted, StateDiscarded, StateExpired:
		return true
	default:
		return false
	}
}
