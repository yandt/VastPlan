// Package kernelspi 定义 Backend Kernel 与部署适配器之间的可替换核心 SPI。
// 接口只描述语义和隔离范围，不绑定环境变量、Vault、SQL 或某个云产品。
package kernelspi

import (
	"context"
	"encoding/json"
	"errors"

	"cdsoft.com.cn/VastPlan/core/shared/go/nodebootstrap"
)

var ErrNotFound = errors.New("kernel SPI 资源不存在")

// Scope 是所有有状态 SPI 的强制隔离键。PluginID 由宿主会话注入，不能信任插件 payload。
type Scope struct {
	TenantID  string
	ProjectID string
	PluginID  string
	Namespace string
}

func (s Scope) Validate() error {
	if s.TenantID == "" || s.PluginID == "" || s.Namespace == "" {
		return errors.New("SPI scope 必须包含 tenant、plugin 和 namespace")
	}
	return nil
}

type ConfigProvider interface {
	Lookup(context.Context, string) (json.RawMessage, bool, error)
}

type CredentialRef struct {
	Name  string `json:"name"`
	Scope string `json:"scope"`
}

// CredentialMaterial 只允许可信宿主适配器在回调期间使用；不得序列化或返回插件。
type CredentialMaterial interface{ Bytes() []byte }

// CredentialBroker 通过回调缩短明文生命周期。插件只能请求具体宿主操作，不能取得 material。
type CredentialBroker interface {
	WithCredential(context.Context, Scope, CredentialRef, func(CredentialMaterial) error) error
}

// DatabaseConnection 是数据库插件交给可信部署适配器的非敏感连接定义。密码不属于
// 此契约，适配器只能通过 CredentialBroker 在受控回调中使用 CredentialRef。
type DatabaseConnection struct {
	Driver      string        `json:"driver"`
	Endpoint    string        `json:"endpoint"`
	Database    string        `json:"database,omitempty"`
	Credentials CredentialRef `json:"credentials"`
}

// DatabaseBroker 在可信宿主内执行数据库连通性检查；它不得将凭证明文返回给插件。
type DatabaseBroker interface {
	Probe(context.Context, Scope, DatabaseConnection) error
}

type Persistence interface {
	Get(context.Context, Scope, string) ([]byte, error)
	Put(context.Context, Scope, string, []byte) error
	Delete(context.Context, Scope, string) error
}

type Transaction interface {
	Persistence
	Commit(context.Context) error
	Rollback(context.Context) error
}

type TransactionManager interface {
	Begin(context.Context, Scope) (Transaction, error)
}

// Dependencies 是一个 backend Host 的可替换依赖集合。nil 表示该能力不可用并 fail-closed。
type Dependencies struct {
	Config        ConfigProvider
	Credentials   CredentialBroker
	Persistence   Persistence
	Transactions  TransactionManager
	Database      DatabaseBroker
	NodeBootstrap nodebootstrap.Broker
}

type MapConfig struct{ values map[string]json.RawMessage }

func NewMapConfig(values map[string]any) (*MapConfig, error) {
	out := &MapConfig{values: map[string]json.RawMessage{}}
	for key, value := range values {
		if key == "" {
			return nil, errors.New("配置 key 不能为空")
		}
		raw, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		out.values[key] = raw
	}
	return out, nil
}

func (m *MapConfig) Lookup(_ context.Context, key string) (json.RawMessage, bool, error) {
	if m == nil {
		return nil, false, nil
	}
	raw, ok := m.values[key]
	return append(json.RawMessage(nil), raw...), ok, nil
}
