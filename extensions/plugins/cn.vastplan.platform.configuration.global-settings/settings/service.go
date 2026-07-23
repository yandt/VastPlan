// Package settings 实现全局设置插件的运行时无关领域逻辑。
package settings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

const (
	PluginID           = "cn.vastplan.platform.configuration.global-settings"
	PluginVersion      = "0.6.0"
	Capability         = "platform.settings"
	StateFileConfigKey = "platform.settings.stateFile"
	changeLimit        = 512
)

var ErrVersionConflict = errors.New("设置版本冲突")

// Service 将每个租户的设置写入部署方指定的持久化文件。leader 放置策略保证同一
// logical service 只有一个写入者；存储卷的复制/故障域仍由部署层负责。
type Service struct {
	mu        sync.Mutex
	state     persistedState
	stateFile string
}

type persistedState struct {
	Tenants map[string]tenantState `json:"tenants"`
}

type tenantState struct {
	Revision int64              `json:"revision"`
	Values   map[string]setting `json:"values"`
	Changes  []change           `json:"changes"`
}

type setting struct {
	Value     json.RawMessage `json:"value"`
	Version   int64           `json:"version"`
	UpdatedAt time.Time       `json:"updatedAt"`
}

type change struct {
	Key       string    `json:"key"`
	Version   int64     `json:"version"`
	Deleted   bool      `json:"deleted"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func New(stateFile string) (*Service, error) {
	s := &Service{state: persistedState{Tenants: map[string]tenantState{}}}
	if strings.TrimSpace(stateFile) != "" {
		if err := s.configure(stateFile); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// configure 只能在还未使用服务时设置状态文件。生产入口经 kernel.config.get
// 读取 unit 配置；测试可直接传入临时文件。
func (s *Service) configure(stateFile string) error {
	if strings.TrimSpace(stateFile) == "" {
		return errors.New("全局设置 stateFile 不能为空")
	}
	if s.stateFile != "" && s.stateFile != stateFile {
		return errors.New("全局设置 stateFile 不允许在运行中切换")
	}
	if s.stateFile != "" {
		return nil
	}
	s.stateFile = stateFile
	return s.load()
}

func (s *Service) ensureConfigured(ctx context.Context, host sdk.Host, callCtx *contractv1.CallContext) error {
	s.mu.Lock()
	configured := s.stateFile != ""
	s.mu.Unlock()
	if configured {
		return nil
	}
	operation := "get"
	request, _ := json.Marshal(map[string]string{"key": StateFileConfigKey})
	result, raw, err := host.Call(ctx, &contractv1.CallTarget{ExtensionPoint: extpoint.KernelService, Capability: "kernel.config.get", Operation: &operation}, callCtx, request)
	if err != nil {
		return fmt.Errorf("读取全局设置部署配置: %w", err)
	}
	if result == nil || result.Status != contractv1.CallResult_STATUS_OK {
		return errors.New("未提供全局设置部署配置")
	}
	var stateFile string
	if err := json.Unmarshal(raw, &stateFile); err != nil || strings.TrimSpace(stateFile) == "" {
		return errors.New("platform.settings.stateFile 必须是非空 JSON 字符串")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.configure(stateFile)
}

func (s *Service) load() error {
	raw, err := os.ReadFile(s.stateFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("读取全局设置状态: %w", err)
	}
	if err := json.Unmarshal(raw, &s.state); err != nil {
		return fmt.Errorf("解析全局设置状态: %w", err)
	}
	if s.state.Tenants == nil {
		s.state.Tenants = map[string]tenantState{}
	}
	return nil
}

func (s *Service) save() error {
	if s.stateFile == "" {
		return errors.New("全局设置尚未配置状态文件")
	}
	raw, err := json.Marshal(s.state)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.stateFile), 0o700); err != nil {
		return fmt.Errorf("创建全局设置状态目录: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(s.stateFile), ".settings-*")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if _, err := temporary.Write(raw); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryName, s.stateFile)
}

func tenantID(callCtx *contractv1.CallContext) (string, error) {
	if callCtx == nil || strings.TrimSpace(callCtx.TenantId) == "" {
		return "", errors.New("全局设置调用必须携带 tenant")
	}
	return callCtx.TenantId, nil
}

func (s *Service) tenant(id string) tenantState {
	tenant := s.state.Tenants[id]
	if tenant.Values == nil {
		tenant.Values = map[string]setting{}
	}
	return tenant
}

func validateKey(key string) error {
	if strings.TrimSpace(key) == "" || len(key) > 320 {
		return errors.New("设置 key 必须为 1-320 个非空字符")
	}
	return nil
}

func (s *Service) Get(callCtx *contractv1.CallContext, key string) (setting, error) {
	if err := validateKey(key); err != nil {
		return setting{}, err
	}
	tenantID, err := tenantID(callCtx)
	if err != nil {
		return setting{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.tenant(tenantID).Values[key]
	if !ok {
		return setting{}, os.ErrNotExist
	}
	value.Value = append(json.RawMessage(nil), value.Value...)
	return value, nil
}

func (s *Service) List(callCtx *contractv1.CallContext, prefix string) map[string]setting {
	tenantID, err := tenantID(callCtx)
	if err != nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]setting{}
	for key, value := range s.tenant(tenantID).Values {
		if strings.HasPrefix(key, prefix) {
			value.Value = append(json.RawMessage(nil), value.Value...)
			out[key] = value
		}
	}
	return out
}

func (s *Service) Put(callCtx *contractv1.CallContext, key string, value json.RawMessage, ifVersion *int64) (setting, error) {
	if err := validateKey(key); err != nil {
		return setting{}, err
	}
	if !json.Valid(value) {
		return setting{}, errors.New("设置 value 必须是有效 JSON")
	}
	tenantID, err := tenantID(callCtx)
	if err != nil {
		return setting{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tenant := s.tenant(tenantID)
	previous, exists := tenant.Values[key]
	if ifVersion != nil && ((!exists && *ifVersion != 0) || (exists && previous.Version != *ifVersion)) {
		return setting{}, ErrVersionConflict
	}
	tenant.Revision++
	next := setting{Value: append(json.RawMessage(nil), value...), Version: tenant.Revision, UpdatedAt: time.Now().UTC()}
	tenant.Values[key] = next
	tenant.Changes = append(tenant.Changes, change{Key: key, Version: next.Version, UpdatedAt: next.UpdatedAt})
	if len(tenant.Changes) > changeLimit {
		tenant.Changes = append([]change(nil), tenant.Changes[len(tenant.Changes)-changeLimit:]...)
	}
	s.state.Tenants[tenantID] = tenant
	if err := s.save(); err != nil {
		return setting{}, err
	}
	return next, nil
}

func (s *Service) Delete(callCtx *contractv1.CallContext, key string, ifVersion *int64) (int64, error) {
	if err := validateKey(key); err != nil {
		return 0, err
	}
	tenantID, err := tenantID(callCtx)
	if err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tenant := s.tenant(tenantID)
	previous, exists := tenant.Values[key]
	if !exists {
		return 0, os.ErrNotExist
	}
	if ifVersion != nil && previous.Version != *ifVersion {
		return 0, ErrVersionConflict
	}
	tenant.Revision++
	delete(tenant.Values, key)
	now := time.Now().UTC()
	tenant.Changes = append(tenant.Changes, change{Key: key, Version: tenant.Revision, Deleted: true, UpdatedAt: now})
	if len(tenant.Changes) > changeLimit {
		tenant.Changes = append([]change(nil), tenant.Changes[len(tenant.Changes)-changeLimit:]...)
	}
	s.state.Tenants[tenantID] = tenant
	if err := s.save(); err != nil {
		return 0, err
	}
	return tenant.Revision, nil
}

func (s *Service) ChangesSince(callCtx *contractv1.CallContext, version int64) ([]change, error) {
	tenantID, err := tenantID(callCtx)
	if err != nil {
		return nil, err
	}
	if version < 0 {
		return nil, errors.New("version 不能小于 0")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tenant := s.tenant(tenantID)
	if len(tenant.Changes) != 0 && version < tenant.Changes[0].Version-1 {
		return nil, errors.New("设置变更游标已过期，请重新 list")
	}
	out := make([]change, 0)
	for _, item := range tenant.Changes {
		if item.Version > version {
			out = append(out, item)
		}
	}
	return out, nil
}

// Handler 返回 SDK 可注册处理器。操作名与签名 manifest 的 tool descriptor 一一对应。
func (s *Service) Handler(ctx context.Context, host sdk.Host, callCtx *contractv1.CallContext, payload []byte, operation string) (*contractv1.CallResult, []byte, error) {
	if err := s.ensureConfigured(ctx, host, callCtx); err != nil {
		return nil, nil, err
	}
	var request struct {
		Key       string          `json:"key"`
		Prefix    string          `json:"prefix"`
		Value     json.RawMessage `json:"value"`
		IfVersion *int64          `json:"ifVersion"`
		Version   int64           `json:"version"`
	}
	if err := json.Unmarshal(payload, &request); err != nil {
		return nil, nil, fmt.Errorf("解析设置请求: %w", err)
	}
	var out any
	var err error
	switch operation {
	case "get":
		var value setting
		value, err = s.Get(callCtx, request.Key)
		out = map[string]any{"key": request.Key, "value": value.Value, "version": value.Version, "updatedAt": value.UpdatedAt}
	case "list":
		values := s.List(callCtx, request.Prefix)
		if values == nil {
			err = errors.New("全局设置调用必须携带 tenant")
		} else {
			keys := make([]string, 0, len(values))
			for key := range values {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			items := make([]map[string]any, 0, len(keys))
			for _, key := range keys {
				value := values[key]
				items = append(items, map[string]any{"key": key, "value": value.Value, "version": value.Version, "updatedAt": value.UpdatedAt})
			}
			out = map[string]any{"items": items}
		}
	case "put":
		var value setting
		value, err = s.Put(callCtx, request.Key, request.Value, request.IfVersion)
		out = map[string]any{"key": request.Key, "version": value.Version, "updatedAt": value.UpdatedAt}
	case "delete":
		var version int64
		version, err = s.Delete(callCtx, request.Key, request.IfVersion)
		out = map[string]any{"key": request.Key, "version": version, "deleted": true}
	case "changesSince":
		var changes []change
		changes, err = s.ChangesSince(callCtx, request.Version)
		out = map[string]any{"changes": changes}
	default:
		err = fmt.Errorf("不支持的全局设置操作 %q", operation)
	}
	if err != nil {
		code := "platform.settings.invalid"
		switch {
		case errors.Is(err, os.ErrNotExist):
			code = "platform.settings.not_found"
		case errors.Is(err, ErrVersionConflict):
			code = "platform.settings.version_conflict"
		}
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: code, Message: err.Error()}}, nil, nil
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return nil, nil, err
	}
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
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
	return sdk.Contribution{ExtensionPoint: extpoint.ToolPackage, ID: Capability, Descriptor: Descriptor(), Handlers: map[string]sdk.Handler{
		"get": func(ctx context.Context, host sdk.Host, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
			return service.Handler(ctx, host, callCtx, payload, "get")
		},
		"list": func(ctx context.Context, host sdk.Host, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
			return service.Handler(ctx, host, callCtx, payload, "list")
		},
		"put": func(ctx context.Context, host sdk.Host, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
			return service.Handler(ctx, host, callCtx, payload, "put")
		},
		"delete": func(ctx context.Context, host sdk.Host, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
			return service.Handler(ctx, host, callCtx, payload, "delete")
		},
		"changesSince": func(ctx context.Context, host sdk.Host, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
			return service.Handler(ctx, host, callCtx, payload, "changesSince")
		},
	}}
}
