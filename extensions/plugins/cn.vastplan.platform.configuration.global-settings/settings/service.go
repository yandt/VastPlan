// Package settings implements tenant-isolated global settings on the trusted
// Shared State service. Business revisions remain distinct from Store CAS revisions.
package settings

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
	sharedstatesdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/sharedstate"
)

const (
	PluginID      = "cn.vastplan.platform.configuration.global-settings"
	PluginVersion = "0.8.0"
	Capability    = "platform.settings"
)

type Service struct{ now func() time.Time }

func New() *Service { return &Service{now: time.Now} }

type request struct {
	Key       string          `json:"key"`
	Prefix    string          `json:"prefix"`
	Value     json.RawMessage `json:"value"`
	IfVersion *int64          `json:"ifVersion"`
	Version   int64           `json:"version"`
}

func (s *Service) Handler(ctx context.Context, host sdk.Host, callCtx *contractv1.CallContext, payload []byte, operation string) (*contractv1.CallResult, []byte, error) {
	if callCtx == nil || callCtx.GetTenantId() == "" {
		return settingsError("platform.settings.invalid", "全局设置调用必须携带 tenant", false), nil, nil
	}
	repository, err := newTenantRepository(host)
	if err != nil {
		return settingsError("platform.settings.unavailable", "Shared State client 不可用", true), nil, nil
	}
	state, storeRevision, err := repository.load(ctx, callCtx)
	if err != nil {
		return settingsError("platform.settings.unavailable", "读取全局设置状态失败", true), nil, nil
	}
	input, err := parseRequest(payload)
	if err != nil {
		return settingsError("platform.settings.invalid", err.Error(), false), nil, nil
	}
	var out any
	write := false
	switch operation {
	case "get":
		var value setting
		value, err = getSetting(state, input.Key)
		out = map[string]any{"key": input.Key, "value": value.Value, "version": value.Version, "updatedAt": value.UpdatedAt}
	case "list":
		out = map[string]any{"items": listSettings(state, input.Prefix)}
	case "put":
		var value setting
		state, value, err = putSetting(state, input.Key, input.Value, input.IfVersion, s.now())
		out, write = map[string]any{"key": input.Key, "version": value.Version, "updatedAt": value.UpdatedAt}, err == nil
	case "delete":
		var version int64
		state, version, err = deleteSetting(state, input.Key, input.IfVersion, s.now())
		out, write = map[string]any{"key": input.Key, "version": version, "deleted": true}, err == nil
	case "changesSince":
		var changes []change
		changes, err = changesSince(state, input.Version)
		out = map[string]any{"changes": changes}
	default:
		err = fmt.Errorf("不支持的全局设置操作 %q", operation)
	}
	if err == nil && write {
		err = repository.save(ctx, callCtx, state, storeRevision)
	}
	if err != nil {
		return mapSettingsError(err), nil, nil
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return nil, nil, err
	}
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
}

func parseRequest(payload []byte) (request, error) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var value request
	if err := decoder.Decode(&value); err != nil {
		return request{}, fmt.Errorf("解析设置请求: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return request{}, errors.New("设置请求只能包含一个 JSON 文档")
	}
	return value, nil
}

func mapSettingsError(err error) *contractv1.CallResult {
	var stateError *sharedstatesdk.ServiceError
	switch {
	case errors.Is(err, ErrNotFound):
		return settingsError("platform.settings.not_found", err.Error(), false)
	case errors.Is(err, ErrVersionConflict):
		return settingsError("platform.settings.version_conflict", err.Error(), true)
	case errors.As(err, &stateError):
		return settingsError("platform.settings.unavailable", "Shared State Provider 不可用", stateError.Retryable)
	default:
		return settingsError("platform.settings.invalid", err.Error(), false)
	}
}

func settingsError(code, message string, retryable bool) *contractv1.CallResult {
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: code, Message: message, Retryable: retryable}}
}

func Descriptor() []byte {
	return []byte(`{"title":"全局设置","subcommands":[
		{"name":"get","description":"读取一个设置","paramsSchema":{"type":"object","properties":{"key":{"type":"string"}},"required":["key"]}},
		{"name":"list","description":"按前缀列出设置","paramsSchema":{"type":"object","properties":{"prefix":{"type":"string"}}}},
		{"name":"put","description":"以可选版本前置条件写入设置","paramsSchema":{"type":"object","properties":{"key":{"type":"string"},"value":{},"ifVersion":{"type":"integer","minimum":0}},"required":["key","value"]}},
		{"name":"delete","description":"以可选版本前置条件删除设置","paramsSchema":{"type":"object","properties":{"key":{"type":"string"},"ifVersion":{"type":"integer","minimum":0}},"required":["key"]}},
		{"name":"changesSince","description":"读取指定版本后的变更","paramsSchema":{"type":"object","properties":{"version":{"type":"integer","minimum":0}},"required":["version"]}}
	]}`)
}

func Contribution(service *Service) sdk.Contribution {
	handler := func(operation string) sdk.Handler {
		return func(ctx context.Context, host sdk.Host, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
			return service.Handler(ctx, host, callCtx, payload, operation)
		}
	}
	return sdk.Contribution{ExtensionPoint: extpoint.ToolPackage, ID: Capability, Descriptor: Descriptor(), Handlers: map[string]sdk.Handler{
		"get": handler("get"), "list": handler("list"), "put": handler("put"), "delete": handler("delete"), "changesSince": handler("changesSince"),
	}}
}
