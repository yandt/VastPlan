// Command connectionmanager 启动数据库连接定义与受控连通性检查插件进程。
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	"cdsoft.com.cn/VastPlan/core/shared/go/kernelspi"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfig"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

const id, version, capability = "cn.vastplan.platform.data.relational.connection-manager", "0.3.0", "platform.database"

const credentialCapability = "platform.credentials"

var errConnectionNotFound = errors.New("数据库连接不存在")

type definition struct {
	Name          string                            `json:"name"`
	Driver        string                            `json:"driver"`
	Endpoint      string                            `json:"endpoint"`
	Database      string                            `json:"database,omitempty"`
	CredentialRef pluginconfig.ManagedCredentialRef `json:"credentialRef"`
}
type credentialStatus struct {
	Managed bool  `json:"managed"`
	Version int64 `json:"version"`
}
type definitionView struct {
	Name       string           `json:"name"`
	Driver     string           `json:"driver"`
	Endpoint   string           `json:"endpoint"`
	Database   string           `json:"database,omitempty"`
	Credential credentialStatus `json:"credential"`
}

func view(value definition) definitionView {
	return definitionView{Name: value.Name, Driver: value.Driver, Endpoint: value.Endpoint, Database: value.Database, Credential: credentialStatus{Managed: value.CredentialRef.Handle != "", Version: value.CredentialRef.Version}}
}

type defineInput struct {
	Name            string `json:"name"`
	Driver          string `json:"driver"`
	Endpoint        string `json:"endpoint"`
	Database        string `json:"database,omitempty"`
	CredentialValue string `json:"credentialValue,omitempty"`
}
type pendingDefinition struct {
	Desired  definition                    `json:"desired"`
	Previous *definition                   `json:"previous,omitempty"`
	Staged   pluginconfig.StagedCredential `json:"staged"`
}
type persisted struct {
	FormatVersion int                                            `json:"formatVersion"`
	Tenants       map[string]map[string]definition               `json:"tenants"`
	Pending       map[string]map[string]pendingDefinition        `json:"pending"`
	Retire        map[string][]pluginconfig.ManagedCredentialRef `json:"retire,omitempty"`
}
type service struct {
	opMu sync.Mutex
	mu   sync.RWMutex
	file string
	data persisted
}

func newService(file string) (*service, error) {
	if file == "" {
		return nil, errors.New("VASTPLAN_DATABASE_CONNECTIONS_STATE_FILE 不能为空")
	}
	s := &service{file: file, data: persisted{FormatVersion: 2, Tenants: map[string]map[string]definition{}, Pending: map[string]map[string]pendingDefinition{}, Retire: map[string][]pluginconfig.ManagedCredentialRef{}}}
	raw, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &s.data); err != nil {
		return nil, err
	}
	if s.data.FormatVersion != 2 {
		return nil, fmt.Errorf("数据库连接状态格式版本 %d 不受支持；开发环境请删除旧状态后重建", s.data.FormatVersion)
	}
	if s.data.Tenants == nil {
		s.data.Tenants = map[string]map[string]definition{}
	}
	if s.data.Pending == nil {
		s.data.Pending = map[string]map[string]pendingDefinition{}
	}
	if s.data.Retire == nil {
		s.data.Retire = map[string][]pluginconfig.ManagedCredentialRef{}
	}
	return s, nil
}

func (s *service) save() error {
	raw, err := json.Marshal(s.data)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.file), 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(s.file), ".connections-*")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
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
	return os.Rename(name, s.file)
}

func (s *service) definitions(t string) map[string]definition {
	if s.data.Tenants[t] == nil {
		s.data.Tenants[t] = map[string]definition{}
	}
	return s.data.Tenants[t]
}
func (s *service) pending(t string) map[string]pendingDefinition {
	if s.data.Pending[t] == nil {
		s.data.Pending[t] = map[string]pendingDefinition{}
	}
	return s.data.Pending[t]
}
func tenant(c *contractv1.CallContext) (string, error) {
	if c == nil || c.TenantId == "" {
		return "", errors.New("数据库调用必须携带 tenant")
	}
	return c.TenantId, nil
}
func callCredential(ctx context.Context, host sdk.Host, call *contractv1.CallContext, operation string, input any, output any) error {
	payload, err := json.Marshal(input)
	if err != nil {
		return err
	}
	logicalService, routingDomain := "platform.credentials", "platform"
	result, raw, err := host.Call(ctx, &contractv1.CallTarget{ExtensionPoint: extpoint.ToolPackage, Capability: credentialCapability, Operation: &operation, LogicalService: &logicalService, RoutingDomain: &routingDomain}, call, payload)
	if err != nil {
		return err
	}
	if result == nil || result.Status != contractv1.CallResult_STATUS_OK {
		if result != nil && result.Error != nil {
			return errors.New(result.Error.Message)
		}
		return errors.New("凭证插件拒绝托管凭证操作")
	}
	if output != nil {
		return json.Unmarshal(raw, output)
	}
	return nil
}

func (s *service) reconcilePending(ctx context.Context, host sdk.Host, call *contractv1.CallContext, t string) error {
	s.mu.RLock()
	items := make(map[string]pendingDefinition, len(s.data.Pending[t]))
	for name, item := range s.data.Pending[t] {
		items[name] = item
	}
	s.mu.RUnlock()
	for name, item := range items {
		if err := callCredential(ctx, host, call, "activateManaged", map[string]string{"stageId": item.Staged.ID}, nil); err != nil {
			return fmt.Errorf("恢复数据库连接 %q 的凭证候选: %w", name, err)
		}
		s.mu.Lock()
		current, ok := s.pending(t)[name]
		if ok && current.Staged.ID == item.Staged.ID {
			old, oldExists := s.definitions(t)[name]
			retireLength := len(s.data.Retire[t])
			s.definitions(t)[name] = item.Desired
			delete(s.pending(t), name)
			if item.Previous != nil && item.Previous.CredentialRef.Handle != "" {
				s.data.Retire[t] = append(s.data.Retire[t], item.Previous.CredentialRef)
			}
			if err := s.save(); err != nil {
				s.pending(t)[name] = current
				s.data.Retire[t] = s.data.Retire[t][:retireLength]
				if oldExists {
					s.definitions(t)[name] = old
				} else {
					delete(s.definitions(t), name)
				}
				s.mu.Unlock()
				return err
			}
		}
		s.mu.Unlock()
	}
	return nil
}

func (s *service) reconcileRetire(ctx context.Context, host sdk.Host, call *contractv1.CallContext, t string) error {
	s.mu.RLock()
	refs := append([]pluginconfig.ManagedCredentialRef(nil), s.data.Retire[t]...)
	s.mu.RUnlock()
	for _, ref := range refs {
		if err := callCredential(ctx, host, call, "retireManaged", map[string]string{"handle": ref.Handle}, nil); err != nil {
			return err
		}
		s.mu.Lock()
		queued := s.data.Retire[t]
		for index := range queued {
			if queued[index].Handle == ref.Handle {
				s.data.Retire[t] = append(queued[:index], queued[index+1:]...)
				break
			}
		}
		if err := s.save(); err != nil {
			s.mu.Unlock()
			return err
		}
		s.mu.Unlock()
	}
	return nil
}

func (s *service) define(ctx context.Context, host sdk.Host, call *contractv1.CallContext, t string, in defineInput) (definition, error) {
	if strings.TrimSpace(in.Name) == "" || len(in.Name) > 160 || strings.TrimSpace(in.Driver) == "" || len(in.Driver) > 64 || strings.TrimSpace(in.Endpoint) == "" || len(in.Endpoint) > 2048 || len(in.Database) > 320 || len(in.CredentialValue) > 4<<20 {
		return definition{}, errors.New("数据库连接字段为空或超过长度上限")
	}
	s.mu.RLock()
	old, exists := s.data.Tenants[t][in.Name]
	s.mu.RUnlock()
	if in.CredentialValue == "" {
		if !exists || old.CredentialRef.Handle == "" {
			return definition{}, errors.New("新连接必须在当前页面输入凭证")
		}
		updated := definition{Name: in.Name, Driver: in.Driver, Endpoint: in.Endpoint, Database: in.Database, CredentialRef: old.CredentialRef}
		s.mu.Lock()
		defer s.mu.Unlock()
		s.definitions(t)[in.Name] = updated
		if err := s.save(); err != nil {
			s.definitions(t)[in.Name] = old
			return definition{}, err
		}
		return updated, nil
	}
	var staged pluginconfig.StagedCredential
	if err := callCredential(ctx, host, call, "stageManaged", map[string]string{"purpose": "database.connection", "resource": in.Name, "value": in.CredentialValue}, &staged); err != nil {
		return definition{}, err
	}
	if staged.ID == "" || staged.Ref.Handle == "" || staged.Ref.Owner != id || staged.Ref.Purpose != "database.connection" || staged.Ref.Scope != "tenant" || staged.Ref.Version < 1 {
		_ = callCredential(ctx, host, call, "abortManaged", map[string]string{"stageId": staged.ID}, nil)
		return definition{}, errors.New("凭证插件返回了不符合当前业务插件边界的引用")
	}
	desired := definition{Name: in.Name, Driver: in.Driver, Endpoint: in.Endpoint, Database: in.Database, CredentialRef: staged.Ref}
	pending := pendingDefinition{Desired: desired, Staged: staged}
	if exists {
		previous := old
		pending.Previous = &previous
	}
	s.mu.Lock()
	s.pending(t)[in.Name] = pending
	if err := s.save(); err != nil {
		delete(s.pending(t), in.Name)
		s.mu.Unlock()
		_ = callCredential(ctx, host, call, "abortManaged", map[string]string{"stageId": staged.ID}, nil)
		return definition{}, err
	}
	s.mu.Unlock()
	if err := callCredential(ctx, host, call, "activateManaged", map[string]string{"stageId": staged.ID}, nil); err != nil {
		return definition{}, err
	}
	s.mu.Lock()
	s.definitions(t)[in.Name] = desired
	delete(s.pending(t), in.Name)
	retireLength := len(s.data.Retire[t])
	if exists && old.CredentialRef.Handle != "" {
		s.data.Retire[t] = append(s.data.Retire[t], old.CredentialRef)
	}
	if err := s.save(); err != nil {
		s.pending(t)[in.Name] = pending
		s.data.Retire[t] = s.data.Retire[t][:retireLength]
		if exists {
			s.definitions(t)[in.Name] = old
		} else {
			delete(s.definitions(t), in.Name)
		}
		s.mu.Unlock()
		return definition{}, err
	}
	s.mu.Unlock()
	_ = s.reconcileRetire(ctx, host, call, t)
	return desired, nil
}

func (s *service) handle(ctx context.Context, host sdk.Host, c *contractv1.CallContext, p []byte, op string) (*contractv1.CallResult, []byte, error) {
	t, err := tenant(c)
	if err != nil {
		return nil, nil, err
	}
	// Configuration writes are infrequent. Serializing the complete local saga
	// prevents two concurrent updates of the same connection from activating an
	// older credential after a newer candidate has already won.
	s.opMu.Lock()
	defer s.opMu.Unlock()
	if err := s.reconcilePending(ctx, host, c, t); err != nil {
		return nil, nil, err
	}
	// Cleanup is durable but does not block reads or a successfully activated
	// replacement when the credential service is temporarily unavailable.
	_ = s.reconcileRetire(ctx, host, c, t)
	var out any
	if op == "define" {
		var in defineInput
		if err := json.Unmarshal(p, &in); err != nil {
			return nil, nil, err
		}
		var saved definition
		saved, err = s.define(ctx, host, c, t, in)
		out = view(saved)
	} else {
		var in struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(p, &in); err != nil {
			return nil, nil, err
		}
		s.mu.RLock()
		defs := s.data.Tenants[t]
		switch op {
		case "describe":
			var ok bool
			var value definition
			value, ok = defs[in.Name]
			if !ok {
				err = errConnectionNotFound
			} else {
				out = view(value)
			}
		case "list":
			items := make([]definitionView, 0, len(defs))
			for _, d := range defs {
				items = append(items, view(d))
			}
			sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
			out = items
		case "remove":
			d, ok := defs[in.Name]
			s.mu.RUnlock()
			if !ok {
				return domainError("platform.database.not_found", errConnectionNotFound)
			}
			s.mu.Lock()
			delete(s.definitions(t), in.Name)
			retireLength := len(s.data.Retire[t])
			s.data.Retire[t] = append(s.data.Retire[t], d.CredentialRef)
			err = s.save()
			if err != nil {
				s.definitions(t)[in.Name] = d
				s.data.Retire[t] = s.data.Retire[t][:retireLength]
			}
			s.mu.Unlock()
			if err == nil {
				_ = s.reconcileRetire(ctx, host, c, t)
			}
			out = map[string]any{"name": in.Name, "removed": err == nil}
			goto marshal
		case "probe":
			d, ok := defs[in.Name]
			s.mu.RUnlock()
			if !ok {
				return domainError("platform.database.not_found", errConnectionNotFound)
			}
			operation := "probe"
			request, _ := json.Marshal(kernelspi.DatabaseConnection{Driver: d.Driver, Endpoint: d.Endpoint, Database: d.Database, Credentials: d.CredentialRef})
			result, raw, callErr := host.Call(ctx, &contractv1.CallTarget{ExtensionPoint: extpoint.KernelService, Capability: "kernel.database.probe", Operation: &operation}, c, request)
			if callErr != nil {
				return nil, nil, callErr
			}
			if result == nil || result.Status != contractv1.CallResult_STATUS_OK {
				return nil, nil, errors.New("可信宿主拒绝数据库连通性检查")
			}
			out = json.RawMessage(raw)
			goto marshal
		default:
			s.mu.RUnlock()
			return nil, nil, errors.New("不支持的数据库操作")
		}
		s.mu.RUnlock()
	}
	if errors.Is(err, errConnectionNotFound) {
		return domainError("platform.database.not_found", err)
	}
	if err != nil {
		return nil, nil, err
	}
marshal:
	raw, err := json.Marshal(out)
	if err != nil {
		return nil, nil, err
	}
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
}

func domainError(code string, err error) (*contractv1.CallResult, []byte, error) {
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: code, Message: err.Error()}}, nil, nil
}
func main() {
	s, err := newService(os.Getenv("VASTPLAN_DATABASE_CONNECTIONS_STATE_FILE"))
	if err != nil {
		log.Fatal(err)
	}
	p := sdk.New(id, version, map[string]string{"backend": "^0.1"})
	handler := func(op string) sdk.Handler {
		return func(ctx context.Context, h sdk.Host, c *contractv1.CallContext, b []byte) (*contractv1.CallResult, []byte, error) {
			return s.handle(ctx, h, c, b, op)
		}
	}
	p.Contribute(sdk.Contribution{ExtensionPoint: extpoint.ToolPackage, ID: capability, Descriptor: descriptor(), Handlers: map[string]sdk.Handler{"define": handler("define"), "describe": handler("describe"), "list": handler("list"), "remove": handler("remove"), "probe": handler("probe")}})
	if err := p.Serve(); err != nil {
		log.Fatal(err)
	}
}

func descriptor() []byte {
	return []byte(`{"title":"数据库连接","subcommands":[
		{"name":"define","description":"定义连接并由插件托管凭证","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"name":{"type":"string"},"driver":{"type":"string"},"endpoint":{"type":"string"},"database":{"type":"string"},"credentialValue":{"type":"string"}},"required":["name","driver","endpoint"]}},
		{"name":"describe","description":"读取连接定义","paramsSchema":{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}},
		{"name":"list","description":"列出连接定义","paramsSchema":{"type":"object","properties":{}}},
		{"name":"remove","description":"删除连接定义","paramsSchema":{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}},
		{"name":"probe","description":"由可信宿主使用凭证探测连接","paramsSchema":{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}}
	]}`)
}
