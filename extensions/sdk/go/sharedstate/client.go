// Package sharedstate is the Go client for the trusted state.shared.v1 kernel
// service. Tenant, plugin and runtime identities never appear in requests.
package sharedstate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	sharedstatev1 "cdsoft.com.cn/VastPlan/contracts/schemas/sharedstate/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

type Client struct {
	host      sdk.Host
	scope     string
	namespace string
}

type ServiceError struct {
	Code      string
	Message   string
	Retryable bool
}

func (e *ServiceError) Error() string { return e.Code + ": " + e.Message }

func New(host sdk.Host, scope, namespace string) (*Client, error) {
	probe, _ := json.Marshal(sharedstatev1.KeyRequest{Scope: scope, Namespace: namespace, Key: "probe"})
	if host == nil {
		return nil, errors.New("Shared State client 缺少宿主")
	}
	if _, err := sharedstatev1.ParseRequest(sharedstatev1.OperationGet, probe); err != nil {
		return nil, errors.New("Shared State scope/namespace 无效")
	}
	return &Client{host: host, scope: scope, namespace: namespace}, nil
}

func (c *Client) Get(ctx context.Context, call *contractv1.CallContext, key string) (sharedstatev1.Entry, error) {
	request := sharedstatev1.KeyRequest{Scope: c.scope, Namespace: c.namespace, Key: key}
	raw, err := c.call(ctx, call, sharedstatev1.OperationGet, request)
	if err != nil {
		return sharedstatev1.Entry{}, err
	}
	return sharedstatev1.ParseEntry(raw)
}

func (c *Client) Create(ctx context.Context, call *contractv1.CallContext, key string, value []byte) (sharedstatev1.Entry, error) {
	request := sharedstatev1.WriteRequest{Scope: c.scope, Namespace: c.namespace, Key: key, Value: sharedstatev1.EncodeValue(value)}
	raw, err := c.call(ctx, call, sharedstatev1.OperationCreate, request)
	if err != nil {
		return sharedstatev1.Entry{}, err
	}
	return sharedstatev1.ParseEntry(raw)
}

func (c *Client) Update(ctx context.Context, call *contractv1.CallContext, key string, value []byte, expected uint64) (sharedstatev1.Entry, error) {
	request := sharedstatev1.WriteRequest{Scope: c.scope, Namespace: c.namespace, Key: key, Value: sharedstatev1.EncodeValue(value), ExpectedRevision: expected}
	raw, err := c.call(ctx, call, sharedstatev1.OperationUpdate, request)
	if err != nil {
		return sharedstatev1.Entry{}, err
	}
	return sharedstatev1.ParseEntry(raw)
}

func (c *Client) Delete(ctx context.Context, call *contractv1.CallContext, key string, expected uint64) error {
	request := sharedstatev1.DeleteRequest{Scope: c.scope, Namespace: c.namespace, Key: key, ExpectedRevision: expected}
	raw, err := c.call(ctx, call, sharedstatev1.OperationDelete, request)
	if err != nil {
		return err
	}
	return sharedstatev1.ParseAck(raw)
}

func (c *Client) List(ctx context.Context, call *contractv1.CallContext, prefix string, limit int, cursor string) (sharedstatev1.Page, error) {
	request := sharedstatev1.ListRequest{Scope: c.scope, Namespace: c.namespace, Prefix: prefix, Limit: limit, PageCursor: cursor}
	raw, err := c.call(ctx, call, sharedstatev1.OperationList, request)
	if err != nil {
		return sharedstatev1.Page{}, err
	}
	return sharedstatev1.ParsePage(raw)
}

func (c *Client) call(ctx context.Context, call *contractv1.CallContext, operation string, request any) ([]byte, error) {
	raw, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	if _, err := sharedstatev1.ParseRequest(operation, raw); err != nil {
		return nil, err
	}
	target := &contractv1.CallTarget{ExtensionPoint: extpoint.KernelService, Capability: sharedstatev1.KernelService(operation)}
	result, response, err := c.host.Call(ctx, target, call, raw)
	if err != nil {
		return nil, err
	}
	if result == nil || result.GetStatus() != contractv1.CallResult_STATUS_OK {
		if result == nil || result.GetError() == nil {
			return nil, errors.New("Shared State 返回空错误")
		}
		return nil, &ServiceError{Code: result.GetError().GetCode(), Message: result.GetError().GetMessage(), Retryable: result.GetError().GetRetryable()}
	}
	if len(response) == 0 {
		return nil, fmt.Errorf("Shared State %s 返回空响应", operation)
	}
	return response, nil
}

func IsConflict(err error) bool {
	var service *ServiceError
	return errors.As(err, &service) && service.Code == "state.conflict"
}

func IsNotFound(err error) bool {
	var service *ServiceError
	return errors.As(err, &service) && service.Code == "state.not_found"
}
