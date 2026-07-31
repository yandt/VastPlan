package versionworkspace

import (
	"context"
	"errors"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	stagingv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionstaging/v1"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
	stagingclient "cdsoft.com.cn/VastPlan/extensions/sdk/go/versionstaging"
)

type hostStaging struct {
	client *stagingclient.Client
	call   *contractv1.CallContext
}

func newHostStaging(host sdk.Host, call *contractv1.CallContext) (*hostStaging, error) {
	client, err := stagingclient.New(host)
	if err != nil {
		return nil, err
	}
	return &hostStaging{client: client, call: call}, nil
}

func (s *hostStaging) BeginUpload(ctx context.Context, request stagingv1.BeginUploadRequest) (stagingv1.UploadStatusResult, error) {
	result, err := s.client.BeginUpload(ctx, s.call, request)
	return result, translateStagingClientError(err)
}

func (s *hostStaging) UploadStatus(ctx context.Context, uploadID string) (stagingv1.UploadStatusResult, error) {
	result, err := s.client.UploadStatus(ctx, s.call, uploadID)
	return result, translateStagingClientError(err)
}

func (s *hostStaging) RenewUpload(ctx context.Context, request stagingv1.RenewUploadRequest) (stagingv1.UploadStatusResult, error) {
	result, err := s.client.RenewUpload(ctx, s.call, request)
	return result, translateStagingClientError(err)
}

func (s *hostStaging) CompleteUpload(ctx context.Context, request stagingv1.UploadRevisionRequest) (stagingv1.UploadStatusResult, error) {
	result, err := s.client.CompleteUpload(ctx, s.call, request)
	return result, translateStagingClientError(err)
}

func (s *hostStaging) AbortUpload(ctx context.Context, request stagingv1.UploadRevisionRequest) (stagingv1.UploadStatusResult, error) {
	result, err := s.client.AbortUpload(ctx, s.call, request)
	return result, translateStagingClientError(err)
}

func translateStagingClientError(err error) error {
	if err == nil {
		return nil
	}
	var serviceErr *stagingclient.ServiceError
	if errors.As(err, &serviceErr) {
		return &StagingError{Code: serviceErr.Code, Retryable: serviceErr.Retryable, Err: err}
	}
	return &StagingError{Code: stagingv1.ErrorStorageUnavailable, Retryable: true, Err: err}
}
