package versionworkspace

import (
	"context"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	workspacev1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionworkspace/v1"
)

func (c *Client) ReadSnapshot(ctx context.Context, call *contractv1.CallContext, sessionID string) (workspacev1.SnapshotResult, error) {
	result, err := c.call(ctx, call, workspacev1.OperationReadSnapshot, workspacev1.SessionRequest{SessionID: sessionID})
	if err != nil {
		return workspacev1.SnapshotResult{}, err
	}
	return *result.(*workspacev1.SnapshotResult), nil
}

func (c *Client) WriteSnapshot(ctx context.Context, call *contractv1.CallContext, request workspacev1.WriteSnapshotRequest) (workspacev1.Session, error) {
	result, err := c.call(ctx, call, workspacev1.OperationWriteSnapshot, request)
	if err != nil {
		return workspacev1.Session{}, err
	}
	return result.(*workspacev1.SessionResult).Session, nil
}

func (c *Client) Changes(ctx context.Context, call *contractv1.CallContext, sessionID string) (workspacev1.ChangesResult, error) {
	result, err := c.call(ctx, call, workspacev1.OperationChanges, workspacev1.SessionRequest{SessionID: sessionID})
	if err != nil {
		return workspacev1.ChangesResult{}, err
	}
	return *result.(*workspacev1.ChangesResult), nil
}

func (c *Client) Commit(ctx context.Context, call *contractv1.CallContext, request workspacev1.CommitRequest) (workspacev1.CommitResult, error) {
	result, err := c.call(ctx, call, workspacev1.OperationCommit, request)
	if err != nil {
		return workspacev1.CommitResult{}, err
	}
	return *result.(*workspacev1.CommitResult), nil
}
