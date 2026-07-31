// Package versioncontent is the Go client for durable version content
// protection. It never exposes physical object locations or credentials.
package versioncontent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/extpoint"
	contentv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versioncontent/v1"
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
		return "Version Content 调用失败"
	}
	return e.Code + ": " + e.Message
}

func New(host sdk.Host) (*Client, error) {
	if host == nil {
		return nil, errors.New("Version Content client 缺少宿主")
	}
	return &Client{host: host}, nil
}

func IsCode(err error, code string) bool {
	var serviceErr *ServiceError
	return errors.As(err, &serviceErr) && serviceErr.Code == code
}

func (c *Client) call(ctx context.Context, call *contractv1.CallContext, operation string, request any) (contentv1.ProtectionResult, error) {
	if c == nil || c.host == nil || call == nil || strings.TrimSpace(call.GetTenantId()) == "" {
		return contentv1.ProtectionResult{}, errors.New("Version Content 调用上下文不完整")
	}
	raw, err := json.Marshal(request)
	if err != nil {
		return contentv1.ProtectionResult{}, err
	}
	parsed, err := contentv1.ParseRequest(operation, raw)
	if err != nil {
		return contentv1.ProtectionResult{}, fmt.Errorf("Version Content 请求无效: %w", err)
	}
	raw, err = json.Marshal(parsed)
	if err != nil {
		return contentv1.ProtectionResult{}, err
	}
	result, response, err := c.host.Call(ctx, &contractv1.CallTarget{ExtensionPoint: extpoint.ToolPackage, Capability: contentv1.Capability, Operation: &operation}, call, raw)
	if err != nil {
		return contentv1.ProtectionResult{}, fmt.Errorf("调用 Version Content %s: %w", operation, err)
	}
	if result == nil || result.Status != contractv1.CallResult_STATUS_OK {
		serviceErr := &ServiceError{Code: contentv1.ErrorStorageUnavailable, Message: "Version Content 拒绝调用", Retryable: true}
		if result != nil && result.Error != nil {
			serviceErr.Code, serviceErr.Message, serviceErr.Retryable = result.Error.Code, result.Error.Message, result.Error.Retryable
		}
		return contentv1.ProtectionResult{}, serviceErr
	}
	parsedResult, err := contentv1.ParseResult(operation, response)
	if err != nil {
		return contentv1.ProtectionResult{}, fmt.Errorf("Version Content 返回无效结果: %w", err)
	}
	return parsedResult, nil
}

func (c *Client) Prepare(ctx context.Context, call *contractv1.CallContext, request contentv1.PrepareRequest) (contentv1.ProtectionResult, error) {
	return c.call(ctx, call, contentv1.OperationPrepare, request)
}

func (c *Client) Status(ctx context.Context, call *contractv1.CallContext, protectionID string) (contentv1.ProtectionResult, error) {
	return c.call(ctx, call, contentv1.OperationStatus, contentv1.StatusRequest{ProtectionID: protectionID})
}

func (c *Client) Confirm(ctx context.Context, call *contractv1.CallContext, request contentv1.ConfirmRequest) (contentv1.ProtectionResult, error) {
	return c.call(ctx, call, contentv1.OperationConfirm, request)
}

func (c *Client) Abort(ctx context.Context, call *contractv1.CallContext, request contentv1.AbortRequest) (contentv1.ProtectionResult, error) {
	return c.call(ctx, call, contentv1.OperationAbort, request)
}
