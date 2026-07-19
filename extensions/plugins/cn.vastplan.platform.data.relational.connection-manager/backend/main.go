// Command connectionmanager 启动数据库连接定义与受控连通性检查插件进程。
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"sort"
	"sync"

	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	"cdsoft.com.cn/VastPlan/core/shared/go/kernelspi"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

const id, version, capability = "cn.vastplan.platform.data.relational.connection-manager", "0.2.0", "platform.database"

var errConnectionNotFound = errors.New("数据库连接不存在")

type definition struct {
	Name       string `json:"name"`
	Driver     string `json:"driver"`
	Endpoint   string `json:"endpoint"`
	Database   string `json:"database,omitempty"`
	Credential string `json:"credential"`
}
type service struct {
	mu       sync.RWMutex
	file     string
	byTenant map[string]map[string]definition
}

func newService(file string) (*service, error) {
	if file == "" {
		return nil, errors.New("VASTPLAN_DATABASE_CONNECTIONS_STATE_FILE 不能为空")
	}
	s := &service{file: file, byTenant: map[string]map[string]definition{}}
	raw, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &s.byTenant); err != nil {
		return nil, err
	}
	if s.byTenant == nil {
		s.byTenant = map[string]map[string]definition{}
	}
	return s, nil
}

func (s *service) save() error {
	raw, err := json.Marshal(s.byTenant)
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
	if s.byTenant[t] == nil {
		s.byTenant[t] = map[string]definition{}
	}
	return s.byTenant[t]
}
func tenant(c *contractv1.CallContext) (string, error) {
	if c == nil || c.TenantId == "" {
		return "", errors.New("数据库调用必须携带 tenant")
	}
	return c.TenantId, nil
}
func (s *service) handle(ctx context.Context, host sdk.Host, c *contractv1.CallContext, p []byte, op string) (*contractv1.CallResult, []byte, error) {
	var in definition
	if err := json.Unmarshal(p, &in); err != nil {
		return nil, nil, err
	}
	t, err := tenant(c)
	if err != nil {
		return nil, nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	defs := s.definitions(t)
	var out any
	switch op {
	case "define":
		if in.Name == "" || in.Driver == "" || in.Endpoint == "" || in.Credential == "" {
			return nil, nil, errors.New("name、driver、endpoint、credential 均不能为空")
		}
		defs[in.Name] = in
		if err := s.save(); err != nil {
			return nil, nil, err
		}
		out = in
	case "describe":
		var ok bool
		out, ok = defs[in.Name]
		if !ok {
			return domainError("platform.database.not_found", errConnectionNotFound)
		}
	case "list":
		items := make([]definition, 0, len(defs))
		for _, d := range defs {
			items = append(items, d)
		}
		sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
		out = items
	case "remove":
		if _, ok := defs[in.Name]; !ok {
			return domainError("platform.database.not_found", errConnectionNotFound)
		}
		delete(defs, in.Name)
		if err := s.save(); err != nil {
			return nil, nil, err
		}
		out = map[string]any{"name": in.Name, "removed": true}
	case "probe":
		d, ok := defs[in.Name]
		if !ok {
			return domainError("platform.database.not_found", errConnectionNotFound)
		}
		operation := "probe"
		request, _ := json.Marshal(kernelspi.DatabaseConnection{Driver: d.Driver, Endpoint: d.Endpoint, Database: d.Database, Credentials: kernelspi.CredentialRef{Name: d.Credential, Scope: "tenant"}})
		result, raw, callErr := host.Call(ctx, &contractv1.CallTarget{ExtensionPoint: extpoint.KernelService, Capability: "kernel.database.probe", Operation: &operation}, c, request)
		if callErr != nil {
			return nil, nil, callErr
		}
		if result == nil || result.Status != contractv1.CallResult_STATUS_OK {
			return nil, nil, errors.New("可信宿主拒绝数据库连通性检查")
		}
		out = json.RawMessage(raw)
	default:
		return nil, nil, errors.New("不支持的数据库操作")
	}
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
		{"name":"define","description":"定义不含密码的连接引用","paramsSchema":{"type":"object","properties":{"name":{"type":"string"},"driver":{"type":"string"},"endpoint":{"type":"string"},"database":{"type":"string"},"credential":{"type":"string"}},"required":["name","driver","endpoint","credential"]}},
		{"name":"describe","description":"读取连接定义","paramsSchema":{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}},
		{"name":"list","description":"列出连接定义","paramsSchema":{"type":"object","properties":{}}},
		{"name":"remove","description":"删除连接定义","paramsSchema":{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}},
		{"name":"probe","description":"由可信宿主使用凭证探测连接","paramsSchema":{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}}
	]}`)
}
