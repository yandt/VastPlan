package versionworkspacev1

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	versioningv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versioning/v1"
	versionresourcev1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionresource/v1"
)

func ParseRequest(operation string, raw []byte) (any, error) {
	var target any
	switch operation {
	case OperationDescribeResource:
		target = &DescribeResourceRequest{}
	case OperationOpen:
		target = &OpenRequest{}
	case OperationStatus, OperationReadSnapshot, OperationChanges:
		target = &SessionRequest{}
	case OperationWriteSnapshot:
		target = &WriteSnapshotRequest{}
	case OperationCommit:
		target = &CommitRequest{}
	case OperationDiscard:
		target = &RevisionRequest{}
	case OperationRenew:
		target = &RenewRequest{}
	case OperationReadCommitted:
		target = &CommittedRequest{}
	case OperationCompareCommitted:
		target = &CompareCommittedRequest{}
	default:
		return nil, fmt.Errorf("不支持的 Version Workspace 操作 %q", operation)
	}
	if err := decodeStrict(raw, target); err != nil {
		return nil, err
	}
	if err := validateRequest(target); err != nil {
		return nil, err
	}
	return target, nil
}

func ParseResult(operation string, raw []byte) (any, error) {
	var target any
	switch operation {
	case OperationDescribeResource:
		target = &ResourceDescription{}
	case OperationOpen, OperationStatus, OperationWriteSnapshot, OperationDiscard, OperationRenew:
		target = &SessionResult{}
	case OperationReadSnapshot:
		target = &SnapshotResult{}
	case OperationChanges:
		target = &ChangesResult{}
	case OperationCommit:
		target = &CommitResult{}
	case OperationReadCommitted:
		target = &CommittedSnapshotResult{}
	case OperationCompareCommitted:
		target = &CompareCommittedResult{}
	default:
		return nil, fmt.Errorf("不支持的 Version Workspace 结果 %q", operation)
	}
	if err := decodeStrict(raw, target); err != nil {
		return nil, err
	}
	if err := validateResult(target); err != nil {
		return nil, err
	}
	return target, nil
}

func validateRequest(target any) error {
	switch request := target.(type) {
	case *DescribeResourceRequest:
		return ValidateDescribeResourceRequest(*request)
	case *OpenRequest:
		return ValidateOpenRequest(*request)
	case *SessionRequest:
		if !sessionPattern.MatchString(request.SessionID) {
			return errors.New("版本工作区 Session 请求无效")
		}
	case *RevisionRequest:
		if !sessionPattern.MatchString(request.SessionID) || request.ExpectedRevision == 0 {
			return errors.New("版本工作区 CAS 请求无效")
		}
	case *WriteSnapshotRequest:
		if !sessionPattern.MatchString(request.SessionID) || request.ExpectedRevision == 0 {
			return errors.New("写入版本工作区快照请求无效")
		}
		return versionresourcev1.ValidateSnapshot(request.Snapshot, MaxWireSnapshotBytes)
	case *CommitRequest:
		return ValidateCommitRequest(*request)
	case *RenewRequest:
		if !sessionPattern.MatchString(request.SessionID) || request.ExpectedRevision == 0 || request.LeaseSeconds < 30 || request.LeaseSeconds > 86400 {
			return errors.New("续租版本工作区请求无效")
		}
	case *CommittedRequest:
		return ValidateCommittedRequest(*request)
	case *CompareCommittedRequest:
		return ValidateCompareCommittedRequest(*request)
	default:
		return errors.New("Version Workspace 请求类型无效")
	}
	return nil
}

func validateResult(target any) error {
	switch result := target.(type) {
	case *ResourceDescription:
		return ValidateResourceDescription(*result)
	case *SessionResult:
		return ValidateSession(result.Session)
	case *SnapshotResult:
		return ValidateSnapshotResult(*result, MaxWireSnapshotBytes)
	case *ChangesResult:
		return ValidateChangesResult(*result)
	case *CommitResult:
		if err := ValidateSession(result.Session); err != nil || result.Session.State != StateCommitted {
			return errors.New("Version Workspace commit Session 无效")
		}
		if err := versioningv1.ValidateVersionRecord(result.Version); err != nil {
			return err
		}
		if result.Version.Ref.Stream.Namespace != result.Session.Namespace || result.Version.Ref.Stream.StreamID != result.Session.Resource.ID {
			return errors.New("Version Workspace commit 版本与资源不匹配")
		}
		if result.Session.BaseRef == nil && len(result.Version.Parents) != 0 || result.Session.BaseRef != nil && (len(result.Version.Parents) == 0 || result.Version.Parents[0] != *result.Session.BaseRef) {
			return errors.New("Version Workspace commit 父版本与 Session 基线不匹配")
		}
		if (result.Session.TargetHead != "") != (result.Head != nil) {
			return errors.New("Version Workspace commit Head 结果缺失或多余")
		}
		if result.Head != nil && (versioningv1.ValidateHead(*result.Head) != nil || result.Head.Target != result.Version.Ref || result.Head.Name != result.Session.TargetHead) {
			return errors.New("Version Workspace commit Head 无效")
		}
	case *CommittedSnapshotResult:
		return ValidateCommittedSnapshotResult(*result, MaxWireSnapshotBytes)
	case *CompareCommittedResult:
		return ValidateCompareCommittedResult(*result)
	default:
		return errors.New("Version Workspace 结果类型无效")
	}
	return nil
}

func decodeStrict(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("Version Workspace 只能包含一个 JSON 文档")
	}
	return nil
}
