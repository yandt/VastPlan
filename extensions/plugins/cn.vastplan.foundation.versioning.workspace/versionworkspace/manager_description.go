package versionworkspace

import (
	"errors"
	"fmt"
	"sort"

	resourcev1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionresource/v1"
	workspacev1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionworkspace/v1"
)

func (m *Manager) DescribeResource(scope Scope, request workspacev1.DescribeResourceRequest) (workspacev1.ResourceDescription, error) {
	if m == nil || scope.Validate() != nil || workspacev1.ValidateDescribeResourceRequest(request) != nil {
		return workspacev1.ResourceDescription{}, workspaceError(workspacev1.ErrorInvalidRequest, false, errors.New("描述 Version Workspace 资源请求无效"))
	}
	environment, binding, adapter, err := m.catalog.resolve(request.EnvironmentID, request.Resource.Type)
	if err != nil {
		return workspacev1.ResourceDescription{}, err
	}
	mode, err := resolveBindingMode(binding, request.RequestedMode)
	if err != nil {
		return workspacev1.ResourceDescription{}, err
	}
	descriptor := adapter.Descriptor()
	allowedModes := append([]string(nil), binding.AllowedModes...)
	sort.Strings(allowedModes)
	maxBytes := resolvedMaxBytes(environment.profile, descriptor, mode)
	description := workspacev1.ResourceDescription{
		Resolution: workspacev1.ResourceResolution{
			EnvironmentID: request.EnvironmentID, EnvironmentDigest: environment.digest, Resource: request.Resource,
			Namespace: binding.Namespace, Adapter: binding.Adapter, Mode: mode,
		},
		ContentKind: descriptor.ContentKind, AllowedModes: allowedModes, DefaultMode: binding.DefaultMode,
		MaxBytes: maxBytes, SecretPolicy: descriptor.SecretPolicy, Capabilities: descriptor.Capabilities,
	}
	if err := workspacev1.ValidateResourceDescription(description); err != nil {
		return workspacev1.ResourceDescription{}, workspaceError(workspacev1.ErrorAdapterUnavailable, false, err)
	}
	return description, nil
}

func resolvedMaxBytes(profile resourcev1.EnvironmentProfile, descriptor resourcev1.AdapterDescriptor, mode string) int64 {
	maxBytes := profile.Limits.MaxSnapshotBytes
	if mode != resourcev1.ModeSnapshot {
		maxBytes = profile.Limits.MaxOverlayBytes
	}
	if descriptor.MaxSnapshotBytes < maxBytes {
		return descriptor.MaxSnapshotBytes
	}
	return maxBytes
}

func resolveBindingMode(binding resourcev1.ResourceBinding, requested string) (string, error) {
	mode := requested
	if mode == "" {
		mode = binding.DefaultMode
	}
	if !containsMode(binding.AllowedModes, mode) {
		return "", workspaceError(workspacev1.ErrorAdapterUnavailable, false, fmt.Errorf("环境不允许资源模式 %q", mode))
	}
	return mode, nil
}
