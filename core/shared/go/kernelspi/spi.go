// Package kernelspi 定义 Backend Kernel 与部署适配器之间的可替换核心 SPI。
// 接口只描述语义和隔离范围，不绑定环境变量、Vault、SQL 或某个云产品。
package kernelspi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"cdsoft.com.cn/VastPlan/core/shared/go/configurationauthority"
	"cdsoft.com.cn/VastPlan/core/shared/go/credentiallease"
	"cdsoft.com.cn/VastPlan/core/shared/go/deploymentpublication"
	"cdsoft.com.cn/VastPlan/core/shared/go/nodebootstrap"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfig"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfiguration"
	"cdsoft.com.cn/VastPlan/core/shared/go/runtimeidentity"
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
	Lookup(context.Context, string, string) (json.RawMessage, bool, error)
}

type CredentialRef = pluginconfig.ManagedCredentialRef

// CredentialMaterial 只允许可信宿主适配器在回调期间使用；不得序列化或返回插件。
type CredentialMaterial interface{ Bytes() []byte }

// CredentialBroker 通过回调缩短明文生命周期。插件只能请求具体宿主操作，不能取得 material。
type CredentialBroker interface {
	WithCredential(context.Context, Scope, CredentialRef, func(CredentialMaterial) error) error
}

// RuntimeMaterialLeaseBroker relays an already encrypted lease to one
// host-authenticated runtime instance. It never opens or returns plaintext.
type RuntimeMaterialLeaseBroker interface {
	IssueRuntimeLease(context.Context, string, runtimeidentity.Identity, credentiallease.Request) (credentiallease.Envelope, error)
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
	Config                         ConfigProvider
	Credentials                    CredentialBroker
	RuntimeMaterialLeases          RuntimeMaterialLeaseBroker
	Persistence                    Persistence
	Transactions                   TransactionManager
	NodeBootstrap                  nodebootstrap.Broker
	NodeReadiness                  nodebootstrap.ReadinessObserver
	DeploymentPublication          deploymentpublication.Controller
	DeploymentReadiness            deploymentpublication.ReadinessObserver
	ConfigurationCatalogs          pluginconfiguration.Reader
	ConfigurationAuthorityIssuer   configurationauthority.Issuer
	ConfigurationAuthorityConsumer configurationauthority.Consumer
}

type MapConfig struct {
	values   map[string]map[string]json.RawMessage
	fallback map[string]json.RawMessage
}

func NewMapConfig(values map[string]any) (*MapConfig, error) {
	frozen, err := freezeConfig(values)
	if err != nil {
		return nil, err
	}
	return &MapConfig{values: map[string]map[string]json.RawMessage{}, fallback: frozen}, nil
}

// NewPluginMapConfig freezes one configuration map per plugin. Unlike
// NewMapConfig it has no fallback namespace and therefore fails closed for an
// unknown caller. Backend Runtime must always use this constructor.
func NewPluginMapConfig(values map[string]map[string]any) (*MapConfig, error) {
	out := &MapConfig{values: map[string]map[string]json.RawMessage{}}
	for pluginID, pluginValues := range values {
		if pluginID == "" {
			return nil, errors.New("配置 plugin id 不能为空")
		}
		frozen, err := freezeConfig(pluginValues)
		if err != nil {
			return nil, fmt.Errorf("冻结插件 %q 配置: %w", pluginID, err)
		}
		out.values[pluginID] = frozen
	}
	return out, nil
}

func freezeConfig(values map[string]any) (map[string]json.RawMessage, error) {
	out := map[string]json.RawMessage{}
	for key, value := range values {
		if key == "" {
			return nil, errors.New("配置 key 不能为空")
		}
		raw, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		out[key] = raw
	}
	return out, nil
}

func (m *MapConfig) Lookup(_ context.Context, pluginID, key string) (json.RawMessage, bool, error) {
	if m == nil {
		return nil, false, nil
	}
	values, ok := m.values[pluginID]
	if !ok {
		values = m.fallback
	}
	raw, ok := values[key]
	return append(json.RawMessage(nil), raw...), ok, nil
}
