package versionworkspace

import (
	"context"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	workspacev1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionworkspace/v1"
)

func (c *Client) Open(ctx context.Context, call *contractv1.CallContext, request workspacev1.OpenRequest) (workspacev1.Session, error) {
	result, err := c.call(ctx, call, workspacev1.OperationOpen, request)
	if err != nil {
		return workspacev1.Session{}, err
	}
	return result.(*workspacev1.SessionResult).Session, nil
}

func (c *Client) Status(ctx context.Context, call *contractv1.CallContext, sessionID string) (workspacev1.Session, error) {
	result, err := c.call(ctx, call, workspacev1.OperationStatus, workspacev1.SessionRequest{SessionID: sessionID})
	if err != nil {
		return workspacev1.Session{}, err
	}
	return result.(*workspacev1.SessionResult).Session, nil
}

func (c *Client) Discard(ctx context.Context, call *contractv1.CallContext, request workspacev1.RevisionRequest) (workspacev1.Session, error) {
	result, err := c.call(ctx, call, workspacev1.OperationDiscard, request)
	if err != nil {
		return workspacev1.Session{}, err
	}
	return result.(*workspacev1.SessionResult).Session, nil
}

func (c *Client) Renew(ctx context.Context, call *contractv1.CallContext, request workspacev1.RenewRequest) (workspacev1.Session, error) {
	result, err := c.call(ctx, call, workspacev1.OperationRenew, request)
	if err != nil {
		return workspacev1.Session{}, err
	}
	return result.(*workspacev1.SessionResult).Session, nil
}
