package versionworkspace

import (
	"context"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	workspacev1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionworkspace/v1"
)

func (c *Client) ReadCommitted(ctx context.Context, call *contractv1.CallContext, request workspacev1.CommittedRequest) (workspacev1.CommittedSnapshotResult, error) {
	result, err := c.call(ctx, call, workspacev1.OperationReadCommitted, request)
	if err != nil {
		return workspacev1.CommittedSnapshotResult{}, err
	}
	return *result.(*workspacev1.CommittedSnapshotResult), nil
}

func (c *Client) CompareCommitted(ctx context.Context, call *contractv1.CallContext, request workspacev1.CompareCommittedRequest) (workspacev1.CompareCommittedResult, error) {
	result, err := c.call(ctx, call, workspacev1.OperationCompareCommitted, request)
	if err != nil {
		return workspacev1.CompareCommittedResult{}, err
	}
	return *result.(*workspacev1.CompareCommittedResult), nil
}
