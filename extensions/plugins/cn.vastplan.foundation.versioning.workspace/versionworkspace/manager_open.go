package versionworkspace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	versioningv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versioning/v1"
	resourcev1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionresource/v1"
	workspacev1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionworkspace/v1"
)

func (m *Manager) Open(ctx context.Context, scope Scope, ledger Ledger, request workspacev1.OpenRequest) (workspacev1.Session, error) {
	if m == nil || ledger == nil || scope.Validate() != nil || workspacev1.ValidateOpenRequest(request) != nil {
		return workspacev1.Session{}, workspaceError(workspacev1.ErrorInvalidRequest, false, errors.New("打开 Version Workspace 的请求或依赖无效"))
	}
	environment, binding, adapter, err := m.catalog.resolve(request.EnvironmentID, request.Resource.Type)
	if err != nil {
		return workspacev1.Session{}, err
	}
	mode := request.RequestedMode
	if mode == "" {
		mode = binding.DefaultMode
	}
	if !containsMode(binding.AllowedModes, mode) {
		return workspacev1.Session{}, workspaceError(workspacev1.ErrorAdapterUnavailable, false, fmt.Errorf("环境不允许资源模式 %q", mode))
	}
	leaseSeconds, err := resolveLeaseSeconds(request.LeaseSeconds, environment.profile.Limits.MaxLeaseSeconds)
	if err != nil {
		return workspacev1.Session{}, err
	}
	m.mu.Lock()
	m.sweepLocked(m.now())
	quotaReached := activeSessionsForTenant(m.sessions, scope.TenantID) >= environment.profile.Limits.MaxSessionsPerTenant
	m.mu.Unlock()
	if quotaReached {
		return workspacev1.Session{}, workspaceError(workspacev1.ErrorLimitExceeded, false, errors.New("租户活动 Version Workspace Session 已达到上限"))
	}
	stream := versioningv1.StreamKey{Namespace: binding.Namespace, StreamID: request.Resource.ID}
	baseRef, baseHead, targetHead, err := resolveOpenReferences(ctx, ledger, stream, request)
	if err != nil {
		return workspacev1.Session{}, err
	}
	base, digest, err := loadBaseSnapshot(ctx, ledger, adapter, binding, request.Resource, mode, baseRef, environment.profile.Limits.MaxSnapshotBytes)
	if err != nil {
		return workspacev1.Session{}, err
	}
	now := m.now().UTC()
	id, err := m.newID()
	if err != nil {
		return workspacev1.Session{}, workspaceError(workspacev1.ErrorAdapterUnavailable, true, fmt.Errorf("生成 Session ID: %w", err))
	}
	session := workspacev1.Session{
		Protocol: workspacev1.Protocol, ID: id, EnvironmentID: request.EnvironmentID, EnvironmentDigest: environment.digest,
		Resource: request.Resource, Namespace: binding.Namespace, Adapter: binding.Adapter, Mode: mode, ReadOnly: request.ReadOnly,
		BaseRef: cloneVersionRef(baseRef), BaseHead: request.BaseHead, TargetHead: request.TargetHead, State: workspacev1.StateClean,
		Revision: 1, CreatedAt: now, LeaseExpiresAt: now.Add(time.Duration(leaseSeconds) * time.Second),
	}
	if targetHead != nil {
		session.HeadRevision = targetHead.Revision
	}
	if baseHead != nil && session.BaseRef == nil {
		return workspacev1.Session{}, workspaceError(workspacev1.ErrorBaseConflict, false, errors.New("BaseHead 未解析为 VersionRef"))
	}
	if err := workspacev1.ValidateSession(session); err != nil {
		return workspacev1.Session{}, workspaceError(workspacev1.ErrorInvalidRequest, false, err)
	}
	record := &sessionRecord{
		session: session, owner: scope, binding: binding, adapter: adapter, maxBytes: environment.profile.Limits.MaxSnapshotBytes, maxLease: environment.profile.Limits.MaxLeaseSeconds,
		base: cloneSnapshot(base), baseDigest: digest, current: cloneSnapshot(base), currentDigest: digest,
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sweepLocked(now)
	if activeSessionsForTenant(m.sessions, scope.TenantID) >= environment.profile.Limits.MaxSessionsPerTenant {
		return workspacev1.Session{}, workspaceError(workspacev1.ErrorLimitExceeded, false, errors.New("租户活动 Version Workspace Session 已达到上限"))
	}
	if _, collision := m.sessions[id]; collision {
		return workspacev1.Session{}, workspaceError(workspacev1.ErrorAdapterUnavailable, true, errors.New("Version Workspace Session ID 冲突"))
	}
	m.sessions[id] = record
	return cloneSession(session), nil
}

func resolveLeaseSeconds(requested, maximum int) (int, error) {
	if requested == 0 {
		requested = defaultLeaseSeconds
		if requested > maximum {
			requested = maximum
		}
	}
	if requested < 30 || requested > maximum {
		return 0, workspaceError(workspacev1.ErrorLimitExceeded, false, fmt.Errorf("Lease 秒数必须在 30..%d", maximum))
	}
	return requested, nil
}

func resolveOpenReferences(ctx context.Context, ledger Ledger, stream versioningv1.StreamKey, request workspacev1.OpenRequest) (*versioningv1.VersionRef, *versioningv1.Head, *versioningv1.Head, error) {
	var baseRef *versioningv1.VersionRef
	var baseHead, targetHead *versioningv1.Head
	if request.BaseRef != nil {
		if request.BaseRef.Stream != stream {
			return nil, nil, nil, workspaceError(workspacev1.ErrorInvalidRequest, false, errors.New("BaseRef 不属于请求资源 stream"))
		}
		ref := *request.BaseRef
		baseRef = &ref
	}
	if request.BaseHead != "" {
		resolved, err := ledger.GetHead(ctx, versioningv1.GetHeadRequest{Stream: stream, Name: request.BaseHead})
		if err != nil {
			return nil, nil, nil, mapLedgerFailure(err, workspacev1.ErrorBaseConflict)
		}
		baseHead = &resolved.Head
		ref := resolved.Head.Target
		baseRef = &ref
	}
	if request.TargetHead != "" {
		resolved, err := ledger.GetHead(ctx, versioningv1.GetHeadRequest{Stream: stream, Name: request.TargetHead})
		if err == nil {
			targetHead = &resolved.Head
		} else if !isLedgerCode(err, versioningv1.ErrorNotFound) {
			return nil, nil, nil, mapLedgerFailure(err, workspacev1.ErrorBaseConflict)
		}
		if baseRef == nil && targetHead != nil {
			ref := targetHead.Target
			baseRef = &ref
		}
	}
	return baseRef, baseHead, targetHead, nil
}

func loadBaseSnapshot(ctx context.Context, ledger Ledger, adapter Adapter, binding resourcev1.ResourceBinding, resource resourcev1.ResourceKey, mode string, baseRef *versioningv1.VersionRef, maxBytes int64) (resourcev1.Snapshot, string, error) {
	snapshot := resourcev1.Snapshot{Kind: resourcev1.ContentJSON, MediaType: "application/json", JSON: json.RawMessage(`{}`)}
	if baseRef != nil {
		_, stored, digest, err := loadCommittedVersion(ctx, ledger, adapter, binding, resource, mode, *baseRef, maxBytes, workspacev1.ErrorBaseConflict)
		return stored, digest, err
	}
	return normalizeSnapshot(ctx, adapter, binding, resource, mode, snapshot, maxBytes)
}

func loadCommittedVersion(ctx context.Context, ledger Ledger, adapter Adapter, binding resourcev1.ResourceBinding, resource resourcev1.ResourceKey, mode string, ref versioningv1.VersionRef, maxBytes int64, notFoundCode string) (versioningv1.VersionRecord, resourcev1.Snapshot, string, error) {
	result, err := ledger.GetVersion(ctx, versioningv1.GetVersionRequest{Ref: ref})
	if err != nil {
		return versioningv1.VersionRecord{}, resourcev1.Snapshot{}, "", mapLedgerFailure(err, notFoundCode)
	}
	snapshot := resourcev1.Snapshot{}
	switch adapter.Descriptor().ContentKind {
	case resourcev1.ContentJSON:
		snapshot = resourcev1.Snapshot{Kind: resourcev1.ContentJSON, MediaType: "application/json", JSON: append(json.RawMessage(nil), result.Version.Content...)}
	case resourcev1.ContentFiles:
		if err := json.Unmarshal(result.Version.Content, &snapshot); err != nil {
			return versioningv1.VersionRecord{}, resourcev1.Snapshot{}, "", workspaceError(workspacev1.ErrorLedgerUnavailable, false, errors.New("Ledger 文件 Snapshot 内容损坏"))
		}
	default:
		return versioningv1.VersionRecord{}, resourcev1.Snapshot{}, "", workspaceError(workspacev1.ErrorAdapterUnavailable, false, errors.New("Adapter 内容类型不受支持"))
	}
	normalized, digest, err := normalizeSnapshot(ctx, adapter, binding, resource, mode, snapshot, maxBytes)
	if err != nil {
		return versioningv1.VersionRecord{}, resourcev1.Snapshot{}, "", err
	}
	if digest != result.Version.Ref.ContentDigest {
		return versioningv1.VersionRecord{}, resourcev1.Snapshot{}, "", workspaceError(workspacev1.ErrorLedgerUnavailable, false, errors.New("Ledger 版本与 Resource Snapshot 摘要不匹配"))
	}
	return result.Version, normalized, digest, nil
}

func normalizeSnapshot(ctx context.Context, adapter Adapter, binding resourcev1.ResourceBinding, resource resourcev1.ResourceKey, mode string, snapshot resourcev1.Snapshot, maxBytes int64) (resourcev1.Snapshot, string, error) {
	normalized, err := adapter.Normalize(ctx, resourcev1.AdapterNormalizeRequest{Resource: resource, Mode: mode, Configuration: binding.AdapterConfig, Snapshot: snapshot})
	if err != nil {
		return resourcev1.Snapshot{}, "", workspaceError(workspacev1.ErrorAdapterUnavailable, false, err)
	}
	if err := resourcev1.ValidateAdapterNormalizeResult(normalized, maxBytes); err != nil {
		return resourcev1.Snapshot{}, "", workspaceError(workspacev1.ErrorAdapterUnavailable, false, err)
	}
	return normalized.Snapshot, normalized.Digest, nil
}

func activeSessionsForTenant(sessions map[string]*sessionRecord, tenantID string) int {
	count := 0
	for _, record := range sessions {
		if record.owner.TenantID == tenantID && (record.session.State == workspacev1.StateClean || record.session.State == workspacev1.StateDirty || record.session.State == workspacev1.StateCommitting) {
			count++
		}
	}
	return count
}

func cloneVersionRef(ref *versioningv1.VersionRef) *versioningv1.VersionRef {
	if ref == nil {
		return nil
	}
	cloned := *ref
	return &cloned
}
