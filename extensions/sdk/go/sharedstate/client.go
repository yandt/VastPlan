// Package sharedstate is the Go client for the trusted state.shared.v1 kernel
// service. Tenant, plugin and runtime identities never appear in requests.
package sharedstate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

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

type Entry struct {
	Key       string
	Value     []byte
	Revision  uint64
	UpdatedAt time.Time
}

type Page struct {
	Items      []Entry
	NextCursor string
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

func (c *Client) Get(ctx context.Context, call *contractv1.CallContext, key string) (Entry, error) {
	request := sharedstatev1.KeyRequest{Scope: c.scope, Namespace: c.namespace, Key: key}
	raw, err := c.call(ctx, call, sharedstatev1.OperationGet, request)
	if err != nil {
		return Entry{}, err
	}
	return parseExpectedEntry(raw, key)
}

func (c *Client) Create(ctx context.Context, call *contractv1.CallContext, key string, value []byte) (Entry, error) {
	request := sharedstatev1.WriteRequest{Scope: c.scope, Namespace: c.namespace, Key: key, Value: sharedstatev1.EncodeValue(value)}
	raw, err := c.call(ctx, call, sharedstatev1.OperationCreate, request)
	if err != nil {
		return Entry{}, err
	}
	return parseExpectedEntry(raw, key)
}

func (c *Client) Update(ctx context.Context, call *contractv1.CallContext, key string, value []byte, expected uint64) (Entry, error) {
	request := sharedstatev1.WriteRequest{Scope: c.scope, Namespace: c.namespace, Key: key, Value: sharedstatev1.EncodeValue(value), ExpectedRevision: expected}
	raw, err := c.call(ctx, call, sharedstatev1.OperationUpdate, request)
	if err != nil {
		return Entry{}, err
	}
	return parseExpectedEntry(raw, key)
}

func (c *Client) Delete(ctx context.Context, call *contractv1.CallContext, key string, expected uint64) error {
	request := sharedstatev1.DeleteRequest{Scope: c.scope, Namespace: c.namespace, Key: key, ExpectedRevision: expected}
	raw, err := c.call(ctx, call, sharedstatev1.OperationDelete, request)
	if err != nil {
		return err
	}
	return sharedstatev1.ParseAck(raw)
}

func (c *Client) List(ctx context.Context, call *contractv1.CallContext, prefix string, limit int, cursor string) (Page, error) {
	request := sharedstatev1.ListRequest{Scope: c.scope, Namespace: c.namespace, Prefix: prefix, Limit: limit, PageCursor: cursor}
	raw, err := c.call(ctx, call, sharedstatev1.OperationList, request)
	if err != nil {
		return Page{}, err
	}
	wire, err := sharedstatev1.ParsePage(raw)
	if err != nil {
		return Page{}, err
	}
	page := Page{Items: make([]Entry, 0, len(wire.Items)), NextCursor: wire.NextPageCursor}
	for _, item := range wire.Items {
		value, err := sharedstatev1.DecodeValue(item.Value)
		if err != nil {
			return Page{}, err
		}
		if !strings.HasPrefix(item.Key, prefix) || item.Key <= cursor || (len(page.Items) != 0 && item.Key <= page.Items[len(page.Items)-1].Key) {
			return Page{}, errors.New("Shared State page 顺序或范围无效")
		}
		page.Items = append(page.Items, Entry{Key: item.Key, Value: value, Revision: item.Revision, UpdatedAt: item.UpdatedAt})
	}
	return page, nil
}

func parseEntry(raw []byte) (Entry, error) {
	wire, err := sharedstatev1.ParseEntry(raw)
	if err != nil {
		return Entry{}, err
	}
	value, err := sharedstatev1.DecodeValue(wire.Value)
	if err != nil {
		return Entry{}, err
	}
	return Entry{Key: wire.Key, Value: value, Revision: wire.Revision, UpdatedAt: wire.UpdatedAt}, nil
}

func parseExpectedEntry(raw []byte, key string) (Entry, error) {
	entry, err := parseEntry(raw)
	if err != nil {
		return Entry{}, err
	}
	if entry.Key != key {
		return Entry{}, errors.New("Shared State entry key 与请求不一致")
	}
	return entry, nil
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
