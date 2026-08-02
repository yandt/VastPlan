package portalcomposer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	versioningv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versioning/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/portalapi"
)

func (s *Service) submitVersionedPortalPublicationLocked(ctx context.Context, principal portalapi.Principal, index int, control portalVersionControlState) (portalapi.PortalPublication, error) {
	revision := s.state.Revisions[index]
	workingCopy, err := s.portalWorkingCopyLocked(principal.TenantID, revision)
	if err != nil {
		return portalapi.PortalPublication{}, err
	}
	operationID := portalVersionOperationID(principal.TenantID, revision.PortalID, revision.ID, revision.WorkingRevision, workingCopy.Digest)
	if control.Pending == nil {
		control.Pending = &portalVersionPendingOperation{
			OperationID: operationID, PublicationID: revision.ID,
			WorkingRevision: revision.WorkingRevision, Digest: workingCopy.Digest,
		}
		s.state.VersionControls[revision.PortalID] = control
		if err := s.save(); err != nil {
			return portalapi.PortalPublication{}, err
		}
	} else if control.Pending.OperationID != operationID || control.Pending.PublicationID != revision.ID || control.Pending.WorkingRevision != revision.WorkingRevision || control.Pending.Digest != workingCopy.Digest {
		return portalapi.PortalPublication{}, fmt.Errorf("%w: Portal 存在另一项待恢复的版本提交", ErrInvalidState)
	}
	request := PortalVersionCommitRequest{
		PortalID: revision.PortalID, Binding: control.Binding, BaseRef: cloneVersionRef(control.LatestRef),
		OperationID: operationID, ActorID: principal.ID, Configuration: workingCopy.Configuration,
	}
	port, err := versionControlFromContext(ctx)
	if err != nil {
		return portalapi.PortalPublication{}, err
	}

	// The external commit cannot run while holding the aggregate mutex. The
	// pending operation was durably saved first and all facts are revalidated
	// after the call before the final aggregate CAS.
	s.mu.Unlock()
	committed, commitErr := port.Commit(ctx, request)
	s.mu.Lock()
	if commitErr != nil {
		return portalapi.PortalPublication{}, commitErr
	}
	if committed.EnvironmentDigest == "" || committed.VersionRef.VersionID == "" {
		return portalapi.PortalPublication{}, fmt.Errorf("%w: Workspace 返回的版本身份无效", ErrVersionControlUnavailable)
	}
	index, err = s.revisionIndex(principal.TenantID, revision.ID)
	if err != nil || s.state.Revisions[index].Status != portalapi.StatusDraft || s.state.Revisions[index].WorkingRevision != revision.WorkingRevision {
		return portalapi.PortalPublication{}, ErrStateConflict
	}
	control, ok := s.state.VersionControls[revision.PortalID]
	if !ok || control.Pending == nil || control.Pending.OperationID != operationID {
		return portalapi.PortalPublication{}, ErrStateConflict
	}
	entry := portalapi.PortalVersionHistoryEntry{
		PublicationID: revision.ID, EnvironmentID: control.Binding.EnvironmentID,
		EnvironmentDigest: committed.EnvironmentDigest, VersionRef: committed.VersionRef,
		ConfigurationDigest: workingCopy.Digest,
		ActorID:             principal.ID, CreatedAt: s.now().UTC().Format(time.RFC3339Nano),
	}
	if !historyContainsOperation(control.History, operationID) {
		control.History = append(control.History, portalVersionHistoryRecord{Entry: entry, OperationID: operationID})
	}
	control.LatestRef = cloneVersionRef(&committed.VersionRef)
	control.Capabilities = committed.Capabilities
	control.Pending = nil
	s.state.VersionControls[revision.PortalID] = control
	return s.transitionPublicationLocked(ctx, principal, index, "submit", "portal.publication.", "")
}

func (s *Service) PortalVersionHistory(_ context.Context, principal portalapi.Principal, portalID string) (portalapi.PortalVersionHistory, error) {
	if err := requireTrustedPrincipal(principal); err != nil {
		return portalapi.PortalVersionHistory{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	control, ok := s.state.VersionControls[portalID]
	if !ok || !s.portalExistsLocked(principal.TenantID, portalID) {
		return portalapi.PortalVersionHistory{}, ErrNotFound
	}
	entries := make([]portalapi.PortalVersionHistoryEntry, 0, len(control.History))
	for _, record := range control.History {
		entries = append(entries, record.Entry)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].PublicationID > entries[j].PublicationID })
	return portalapi.PortalVersionHistory{PortalID: portalID, Entries: entries}, nil
}

func (s *Service) ReadPortalVersion(ctx context.Context, principal portalapi.Principal, portalID, versionID string) (portalapi.PortalVersionSnapshot, error) {
	control, entry, err := s.portalVersionHistoryEntry(principal, portalID, versionID)
	if err != nil {
		return portalapi.PortalVersionSnapshot{}, err
	}
	port, err := versionControlFromContext(ctx)
	if err != nil {
		return portalapi.PortalVersionSnapshot{}, err
	}
	configuration, err := port.Read(ctx, PortalVersionReadRequest{
		PortalID: portalID, Binding: control.Binding, EnvironmentDigest: entry.EnvironmentDigest, VersionRef: entry.VersionRef,
	})
	if err != nil {
		return portalapi.PortalVersionSnapshot{}, err
	}
	return portalapi.PortalVersionSnapshot{Entry: entry, Configuration: configuration}, nil
}

func (s *Service) ComparePortalVersions(ctx context.Context, principal portalapi.Principal, portalID, leftVersionID, rightVersionID string) (portalapi.PortalVersionComparison, error) {
	control, left, err := s.portalVersionHistoryEntry(principal, portalID, leftVersionID)
	if err != nil {
		return portalapi.PortalVersionComparison{}, err
	}
	_, right, err := s.portalVersionHistoryEntry(principal, portalID, rightVersionID)
	if err != nil {
		return portalapi.PortalVersionComparison{}, err
	}
	if left.EnvironmentDigest != right.EnvironmentDigest {
		return portalapi.PortalVersionComparison{}, fmt.Errorf("%w: 两个版本属于不同的环境修订，不能直接比较", ErrInvalidState)
	}
	port, err := versionControlFromContext(ctx)
	if err != nil {
		return portalapi.PortalVersionComparison{}, err
	}
	comparison, err := port.Compare(ctx, PortalVersionCompareRequest{
		PortalID: portalID, Binding: control.Binding, EnvironmentDigest: left.EnvironmentDigest,
		Left: left.VersionRef, Right: right.VersionRef,
	})
	if err != nil {
		return portalapi.PortalVersionComparison{}, err
	}
	return portalapi.PortalVersionComparison{
		Left: left, Right: right, Dirty: comparison.Dirty, DiffAvailable: comparison.DiffAvailable,
		ChangedPaths: append([]string(nil), comparison.ChangedPaths...), Summary: comparison.Summary,
	}, nil
}

func (s *Service) RestorePortalVersion(ctx context.Context, principal portalapi.Principal, portalID string, request portalapi.RestorePortalVersionRequest) (portalapi.PortalWorkingCopy, error) {
	if request.ExpectedWorkingRevision == 0 {
		return portalapi.PortalWorkingCopy{}, ErrInvalidState
	}
	snapshot, err := s.ReadPortalVersion(ctx, principal, portalID, request.VersionID)
	if err != nil {
		return portalapi.PortalWorkingCopy{}, err
	}
	return s.SavePortalWorkingCopy(ctx, principal, portalID, portalapi.SavePortalWorkingCopyRequest{
		ExpectedRevision: request.ExpectedWorkingRevision, Configuration: snapshot.Configuration,
	})
}

func (s *Service) portalVersionHistoryEntry(principal portalapi.Principal, portalID, versionID string) (portalVersionControlState, portalapi.PortalVersionHistoryEntry, error) {
	if err := requireTrustedPrincipal(principal); err != nil {
		return portalVersionControlState{}, portalapi.PortalVersionHistoryEntry{}, err
	}
	versionID = strings.TrimSpace(versionID)
	if versionID == "" {
		return portalVersionControlState{}, portalapi.PortalVersionHistoryEntry{}, ErrNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	control, ok := s.state.VersionControls[portalID]
	if !ok || !s.portalExistsLocked(principal.TenantID, portalID) {
		return portalVersionControlState{}, portalapi.PortalVersionHistoryEntry{}, ErrNotFound
	}
	for _, record := range control.History {
		entry := record.Entry
		if entry.VersionRef.VersionID == versionID {
			return control, entry, nil
		}
	}
	return portalVersionControlState{}, portalapi.PortalVersionHistoryEntry{}, ErrNotFound
}

func portalVersionOperationID(tenantID, portalID string, publicationID, workingRevision uint64, digest string) string {
	identity := strings.Join([]string{tenantID, portalID, strconv.FormatUint(publicationID, 10), strconv.FormatUint(workingRevision, 10), digest}, "\x00")
	sum := sha256.Sum256([]byte(identity))
	return "portal-publication:" + hex.EncodeToString(sum[:])
}

func cloneVersionRef(ref *versioningv1.VersionRef) *versioningv1.VersionRef {
	if ref == nil {
		return nil
	}
	value := *ref
	return &value
}

func historyContainsOperation(entries []portalVersionHistoryRecord, operationID string) bool {
	for _, entry := range entries {
		if entry.OperationID == operationID {
			return true
		}
	}
	return false
}
