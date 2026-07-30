package versionworkspace

import (
	"context"
	"errors"

	resourcev1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionresource/v1"
	workspacev1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionworkspace/v1"
)

type diffDetails struct {
	available bool
	paths     []string
	summary   workspacev1.ChangeSummary
}

func calculateDiff(ctx context.Context, adapter Adapter, request resourcev1.AdapterDiffRequest, dirty bool) (diffDetails, error) {
	if !adapter.Descriptor().Capabilities.Diff {
		return diffDetails{}, nil
	}
	differ, ok := adapter.(DiffAdapter)
	if !ok {
		return diffDetails{}, workspaceError(workspacev1.ErrorAdapterUnavailable, false, errors.New("Resource Adapter 声明 diff 但未实现"))
	}
	result, err := differ.Diff(ctx, request)
	if err != nil {
		return diffDetails{}, workspaceError(workspacev1.ErrorAdapterUnavailable, false, err)
	}
	if err := resourcev1.ValidateAdapterDiffResult(result); err != nil {
		return diffDetails{}, workspaceError(workspacev1.ErrorAdapterUnavailable, false, err)
	}
	if dirty != (result.Summary.Total > 0) {
		return diffDetails{}, workspaceError(workspacev1.ErrorAdapterUnavailable, false, errors.New("Resource Adapter diff 与规范摘要不一致"))
	}
	return diffDetails{available: true, paths: append([]string(nil), result.ChangedPaths...), summary: result.Summary}, nil
}
