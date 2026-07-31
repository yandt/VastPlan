// Package versionstaging is the Go control-plane client for lease-bound
// content staging. File bytes always use a separate Host streaming API.
package versionstaging

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/extpoint"
	stagingv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionstaging/v1"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

type Client struct{ host sdk.Host }

type ServiceError struct {
	Code      string
	Message   string
	Retryable bool
}

func (e *ServiceError) Error() string {
	if e == nil {
		return "Version Staging 调用失败"
	}
	return e.Code + ": " + e.Message
}

func New(host sdk.Host) (*Client, error) {
	if host == nil {
		return nil, errors.New("Version Staging client 缺少宿主")
	}
	return &Client{host: host}, nil
}

func IsCode(err error, code string) bool {
	var serviceErr *ServiceError
	return errors.As(err, &serviceErr) && serviceErr.Code == code
}

func (c *Client) call(ctx context.Context, call *contractv1.CallContext, operation string, request any) (stagingv1.UploadStatusResult, error) {
	if c == nil || c.host == nil || call == nil || strings.TrimSpace(call.GetTenantId()) == "" {
		return stagingv1.UploadStatusResult{}, errors.New("Version Staging 调用上下文不完整")
	}
	raw, err := json.Marshal(request)
	if err != nil {
		return stagingv1.UploadStatusResult{}, err
	}
	parsed, err := stagingv1.ParseRequest(operation, raw)
	if err != nil {
		return stagingv1.UploadStatusResult{}, fmt.Errorf("Version Staging 请求无效: %w", err)
	}
	raw, err = json.Marshal(parsed)
	if err != nil {
		return stagingv1.UploadStatusResult{}, err
	}
	result, response, err := c.host.Call(ctx, &contractv1.CallTarget{
		ExtensionPoint: extpoint.ToolPackage, Capability: stagingv1.Capability, Operation: &operation,
	}, call, raw)
	if err != nil {
		return stagingv1.UploadStatusResult{}, fmt.Errorf("调用 Version Staging %s: %w", operation, err)
	}
	if result == nil || result.Status != contractv1.CallResult_STATUS_OK {
		serviceErr := &ServiceError{Code: stagingv1.ErrorStorageUnavailable, Message: "Version Staging 拒绝调用", Retryable: true}
		if result != nil && result.Error != nil {
			serviceErr.Code, serviceErr.Message, serviceErr.Retryable = result.Error.Code, result.Error.Message, result.Error.Retryable
		}
		return stagingv1.UploadStatusResult{}, serviceErr
	}
	parsedResult, err := stagingv1.ParseResult(operation, response)
	if err != nil {
		return stagingv1.UploadStatusResult{}, fmt.Errorf("Version Staging 返回无效结果: %w", err)
	}
	return parsedResult, nil
}
