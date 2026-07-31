package versionstaging

import (
	"context"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	stagingv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionstaging/v1"
)

func (c *Client) BeginUpload(ctx context.Context, call *contractv1.CallContext, request stagingv1.BeginUploadRequest) (stagingv1.UploadStatusResult, error) {
	return c.call(ctx, call, stagingv1.OperationBeginUpload, request)
}

func (c *Client) UploadStatus(ctx context.Context, call *contractv1.CallContext, uploadID string) (stagingv1.UploadStatusResult, error) {
	return c.call(ctx, call, stagingv1.OperationUploadStatus, stagingv1.UploadStatusRequest{UploadID: uploadID})
}

func (c *Client) RenewUpload(ctx context.Context, call *contractv1.CallContext, request stagingv1.RenewUploadRequest) (stagingv1.UploadStatusResult, error) {
	return c.call(ctx, call, stagingv1.OperationRenewUpload, request)
}

func (c *Client) CompleteUpload(ctx context.Context, call *contractv1.CallContext, request stagingv1.UploadRevisionRequest) (stagingv1.UploadStatusResult, error) {
	return c.call(ctx, call, stagingv1.OperationCompleteUpload, request)
}

func (c *Client) AbortUpload(ctx context.Context, call *contractv1.CallContext, request stagingv1.UploadRevisionRequest) (stagingv1.UploadStatusResult, error) {
	return c.call(ctx, call, stagingv1.OperationAbortUpload, request)
}
