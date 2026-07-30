// Package versionworkspace is the Go client for lease-bound version editing
// environments. Consumers never address Ledger Providers or storage endpoints.
package versionworkspace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/extpoint"
	workspacev1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionworkspace/v1"
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
		return "Version Workspace 调用失败"
	}
	return e.Code + ": " + e.Message
}

func New(host sdk.Host) (*Client, error) {
	if host == nil {
		return nil, errors.New("Version Workspace client 缺少宿主")
	}
	return &Client{host: host}, nil
}

func IsCode(err error, code string) bool {
	var serviceErr *ServiceError
	return errors.As(err, &serviceErr) && serviceErr.Code == code
}

func (c *Client) call(ctx context.Context, call *contractv1.CallContext, operation string, request any) (any, error) {
	if c == nil || c.host == nil || call == nil || strings.TrimSpace(call.GetTenantId()) == "" {
		return nil, errors.New("Version Workspace 调用上下文不完整")
	}
	raw, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	parsed, err := workspacev1.ParseRequest(operation, raw)
	if err != nil {
		return nil, fmt.Errorf("Version Workspace 请求无效: %w", err)
	}
	raw, err = json.Marshal(parsed)
	if err != nil {
		return nil, err
	}
	result, response, err := c.host.Call(ctx, &contractv1.CallTarget{
		ExtensionPoint: extpoint.ToolPackage, Capability: workspacev1.Capability, Operation: &operation,
	}, call, raw)
	if err != nil {
		return nil, fmt.Errorf("调用 Version Workspace %s: %w", operation, err)
	}
	if result == nil || result.Status != contractv1.CallResult_STATUS_OK {
		serviceErr := &ServiceError{Code: workspacev1.ErrorLedgerUnavailable, Message: "Version Workspace 拒绝调用", Retryable: true}
		if result != nil && result.Error != nil {
			serviceErr.Code, serviceErr.Message, serviceErr.Retryable = result.Error.Code, result.Error.Message, result.Error.Retryable
		}
		return nil, serviceErr
	}
	parsedResult, err := workspacev1.ParseResult(operation, response)
	if err != nil {
		return nil, fmt.Errorf("Version Workspace 返回无效结果: %w", err)
	}
	return parsedResult, nil
}
