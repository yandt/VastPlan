package versionworkspace

import (
	"context"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	workspacev1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionworkspace/v1"
)

func (c *Client) BeginContentUpload(ctx context.Context, call *contractv1.CallContext, request workspacev1.BeginContentUploadRequest) (workspacev1.ContentUploadResult, error) {
	return c.contentUploadCall(ctx, call, workspacev1.OperationBeginContentUpload, request)
}

func (c *Client) ContentUploadStatus(ctx context.Context, call *contractv1.CallContext, request workspacev1.ContentUploadRequest) (workspacev1.ContentUploadResult, error) {
	return c.contentUploadCall(ctx, call, workspacev1.OperationContentUploadStatus, request)
}

func (c *Client) RenewContentUpload(ctx context.Context, call *contractv1.CallContext, request workspacev1.RenewContentUploadRequest) (workspacev1.ContentUploadResult, error) {
	return c.contentUploadCall(ctx, call, workspacev1.OperationRenewContentUpload, request)
}

func (c *Client) CompleteContentUpload(ctx context.Context, call *contractv1.CallContext, request workspacev1.ContentUploadRevisionRequest) (workspacev1.ContentUploadResult, error) {
	return c.contentUploadCall(ctx, call, workspacev1.OperationCompleteContentUpload, request)
}

func (c *Client) AbortContentUpload(ctx context.Context, call *contractv1.CallContext, request workspacev1.ContentUploadRevisionRequest) (workspacev1.ContentUploadResult, error) {
	return c.contentUploadCall(ctx, call, workspacev1.OperationAbortContentUpload, request)
}

func (c *Client) contentUploadCall(ctx context.Context, call *contractv1.CallContext, operation string, request any) (workspacev1.ContentUploadResult, error) {
	result, err := c.call(ctx, call, operation, request)
	if err != nil {
		return workspacev1.ContentUploadResult{}, err
	}
	return *result.(*workspacev1.ContentUploadResult), nil
}
