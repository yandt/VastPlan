package versionworkspace

import (
	"context"
	"errors"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	contentv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versioncontent/v1"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
	contentclient "cdsoft.com.cn/VastPlan/extensions/sdk/go/versioncontent"
)

type hostContentReference struct {
	client *contentclient.Client
	call   *contractv1.CallContext
}

func newHostContentReference(host sdk.Host, call *contractv1.CallContext) (*hostContentReference, error) {
	client, err := contentclient.New(host)
	if err != nil {
		return nil, err
	}
	return &hostContentReference{client: client, call: call}, nil
}

func (r *hostContentReference) Prepare(ctx context.Context, request contentv1.PrepareRequest) (contentv1.ProtectionResult, error) {
	result, err := r.client.Prepare(ctx, r.call, request)
	return result, translateContentReferenceClientError(err)
}

func (r *hostContentReference) Status(ctx context.Context, protectionID string) (contentv1.ProtectionResult, error) {
	result, err := r.client.Status(ctx, r.call, protectionID)
	return result, translateContentReferenceClientError(err)
}

func (r *hostContentReference) Confirm(ctx context.Context, request contentv1.ConfirmRequest) (contentv1.ProtectionResult, error) {
	result, err := r.client.Confirm(ctx, r.call, request)
	return result, translateContentReferenceClientError(err)
}

func (r *hostContentReference) Abort(ctx context.Context, request contentv1.AbortRequest) (contentv1.ProtectionResult, error) {
	result, err := r.client.Abort(ctx, r.call, request)
	return result, translateContentReferenceClientError(err)
}

func translateContentReferenceClientError(err error) error {
	if err == nil {
		return nil
	}
	var serviceErr *contentclient.ServiceError
	if errors.As(err, &serviceErr) {
		return &ContentReferenceError{Code: serviceErr.Code, Retryable: serviceErr.Retryable, Err: err}
	}
	return &ContentReferenceError{Code: contentv1.ErrorStorageUnavailable, Retryable: true, Err: err}
}
