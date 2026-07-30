package versionworkspace

import (
	"context"
	"errors"
	"fmt"

	versioningv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versioning/v1"
	resourcev1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionresource/v1"
	workspacev1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionworkspace/v1"
)

type committedContext struct {
	resolution workspacev1.ResourceResolution
	binding    resourcev1.ResourceBinding
	adapter    Adapter
	maxBytes   int64
}

func (m *Manager) ReadCommitted(ctx context.Context, scope Scope, ledger Ledger, request workspacev1.CommittedRequest) (workspacev1.CommittedSnapshotResult, error) {
	if m == nil || ledger == nil || scope.Validate() != nil || workspacev1.ValidateCommittedRequest(request) != nil {
		return workspacev1.CommittedSnapshotResult{}, workspaceError(workspacev1.ErrorInvalidRequest, false, errors.New("读取已提交 Version Workspace 版本请求无效"))
	}
	resolved, err := m.resolveCommittedContext(request.EnvironmentID, request.EnvironmentDigest, request.Resource, request.RequestedMode, request.Ref.Stream)
	if err != nil {
		return workspacev1.CommittedSnapshotResult{}, err
	}
	version, snapshot, digest, err := loadCommittedVersion(ctx, ledger, resolved.adapter, resolved.binding, request.Resource, resolved.resolution.Mode, request.Ref, resolved.maxBytes, workspacev1.ErrorVersionNotFound)
	if err != nil {
		return workspacev1.CommittedSnapshotResult{}, err
	}
	result := workspacev1.CommittedSnapshotResult{Resolution: resolved.resolution, Version: cloneVersionRecord(version), Snapshot: cloneSnapshot(snapshot), Digest: digest}
	if err := workspacev1.ValidateCommittedSnapshotResult(result, resolved.maxBytes); err != nil {
		return workspacev1.CommittedSnapshotResult{}, workspaceError(workspacev1.ErrorAdapterUnavailable, false, err)
	}
	return result, nil
}

func (m *Manager) CompareCommitted(ctx context.Context, scope Scope, ledger Ledger, request workspacev1.CompareCommittedRequest) (workspacev1.CompareCommittedResult, error) {
	if m == nil || ledger == nil || scope.Validate() != nil || workspacev1.ValidateCompareCommittedRequest(request) != nil {
		return workspacev1.CompareCommittedResult{}, workspaceError(workspacev1.ErrorInvalidRequest, false, errors.New("比较已提交 Version Workspace 版本请求无效"))
	}
	resolved, err := m.resolveCommittedContext(request.EnvironmentID, request.EnvironmentDigest, request.Resource, request.RequestedMode, request.Left.Stream)
	if err != nil {
		return workspacev1.CompareCommittedResult{}, err
	}
	_, left, leftDigest, err := loadCommittedVersion(ctx, ledger, resolved.adapter, resolved.binding, request.Resource, resolved.resolution.Mode, request.Left, resolved.maxBytes, workspacev1.ErrorVersionNotFound)
	if err != nil {
		return workspacev1.CompareCommittedResult{}, err
	}
	_, right, rightDigest, err := loadCommittedVersion(ctx, ledger, resolved.adapter, resolved.binding, request.Resource, resolved.resolution.Mode, request.Right, resolved.maxBytes, workspacev1.ErrorVersionNotFound)
	if err != nil {
		return workspacev1.CompareCommittedResult{}, err
	}
	diff, err := resolved.adapter.Diff(ctx, resourcev1.AdapterDiffRequest{Resource: request.Resource, Mode: resolved.resolution.Mode, Left: left, Right: right})
	if err != nil {
		return workspacev1.CompareCommittedResult{}, workspaceError(workspacev1.ErrorAdapterUnavailable, false, err)
	}
	if err := resourcev1.ValidateAdapterDiffResult(diff); err != nil {
		return workspacev1.CompareCommittedResult{}, workspaceError(workspacev1.ErrorAdapterUnavailable, false, err)
	}
	result := workspacev1.CompareCommittedResult{
		Resolution: resolved.resolution, Left: request.Left, Right: request.Right, LeftDigest: leftDigest, RightDigest: rightDigest,
		Dirty: leftDigest != rightDigest, DiffAvailable: true, ChangedPaths: append([]string(nil), diff.ChangedPaths...), Summary: diff.Summary,
	}
	if err := workspacev1.ValidateCompareCommittedResult(result); err != nil {
		return workspacev1.CompareCommittedResult{}, workspaceError(workspacev1.ErrorAdapterUnavailable, false, err)
	}
	return result, nil
}

func (m *Manager) resolveCommittedContext(environmentID, environmentDigest string, resource resourcev1.ResourceKey, requestedMode string, stream versioningv1.StreamKey) (committedContext, error) {
	environment, binding, adapter, err := m.catalog.resolveExact(environmentID, environmentDigest, resource.Type)
	if err != nil {
		return committedContext{}, err
	}
	mode := requestedMode
	if mode == "" {
		mode = binding.DefaultMode
	}
	if !containsMode(binding.AllowedModes, mode) {
		return committedContext{}, workspaceError(workspacev1.ErrorAdapterUnavailable, false, fmt.Errorf("环境不允许资源模式 %q", mode))
	}
	expected := versioningv1.StreamKey{Namespace: binding.Namespace, StreamID: resource.ID}
	if stream != expected {
		return committedContext{}, workspaceError(workspacev1.ErrorInvalidRequest, false, errors.New("VersionRef 不属于请求资源 stream"))
	}
	resolution := workspacev1.ResourceResolution{
		EnvironmentID: environmentID, EnvironmentDigest: environment.digest, Resource: resource,
		Namespace: binding.Namespace, Adapter: binding.Adapter, Mode: mode,
	}
	return committedContext{resolution: resolution, binding: binding, adapter: adapter, maxBytes: environment.profile.Limits.MaxSnapshotBytes}, nil
}
