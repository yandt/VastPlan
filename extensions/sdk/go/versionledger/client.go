// Package versionledger provides the framework-neutral Go client for the
// version.ledger.v1 capability. It never imports a concrete Provider plugin.
package versionledger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/extpoint"
	versioningv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versioning/v1"
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
		return "Version Ledger 调用失败"
	}
	return e.Code + ": " + e.Message
}

func New(host sdk.Host) (*Client, error) {
	if host == nil {
		return nil, errors.New("Version Ledger client 缺少宿主")
	}
	return &Client{host: host}, nil
}

func IsCode(err error, code string) bool {
	var serviceErr *ServiceError
	return errors.As(err, &serviceErr) && serviceErr.Code == code
}

func (c *Client) call(ctx context.Context, call *contractv1.CallContext, operation string, request any) (any, error) {
	if c == nil || c.host == nil || call == nil || strings.TrimSpace(call.GetTenantId()) == "" {
		return nil, errors.New("Version Ledger 调用上下文不完整")
	}
	raw, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	parsed, err := versioningv1.ParseRequest(operation, raw)
	if err != nil {
		return nil, fmt.Errorf("Version Ledger 请求无效: %w", err)
	}
	raw, err = json.Marshal(parsed)
	if err != nil {
		return nil, err
	}
	result, response, err := c.host.Call(ctx, &contractv1.CallTarget{
		ExtensionPoint: extpoint.ToolPackage, Capability: versioningv1.LedgerCapability, Operation: &operation,
	}, call, raw)
	if err != nil {
		return nil, fmt.Errorf("调用 Version Ledger %s: %w", operation, err)
	}
	if result == nil || result.Status != contractv1.CallResult_STATUS_OK {
		serviceErr := &ServiceError{Code: versioningv1.ErrorProviderUnavailable, Message: "Version Ledger 拒绝调用", Retryable: true}
		if result != nil && result.Error != nil {
			serviceErr.Code, serviceErr.Message, serviceErr.Retryable = result.Error.Code, result.Error.Message, result.Error.Retryable
		}
		return nil, serviceErr
	}
	parsedResult, err := versioningv1.ParseResult(operation, response)
	if err != nil {
		return nil, fmt.Errorf("Version Ledger 返回无效结果: %w", err)
	}
	return parsedResult, nil
}
