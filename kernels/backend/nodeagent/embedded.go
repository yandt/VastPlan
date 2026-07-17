package nodeagent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"cdsoft.com.cn/VastPlan/shared/go/protocolbus"
)

type EmbeddedFactory func() protocolbus.EmbeddedPlugin

type embeddedCatalogEntry struct {
	id, version string
	factory     EmbeddedFactory
}

// EmbeddedCatalog 是内核编译时静态目录。远端制品和插件清单都不能向它动态写入，
// 因而“被验签”不等于“可进入内核进程”。
type EmbeddedCatalog struct {
	mu      sync.RWMutex
	entries map[string]embeddedCatalogEntry
}

func NewEmbeddedCatalog(factories ...EmbeddedFactory) (*EmbeddedCatalog, error) {
	catalog := &EmbeddedCatalog{entries: map[string]embeddedCatalogEntry{}}
	for _, factory := range factories {
		if err := catalog.Register(factory); err != nil {
			return nil, err
		}
	}
	return catalog, nil
}

func embeddedCatalogKey(id, version string) string { return id + "\x00" + version }

func (c *EmbeddedCatalog) Register(factory EmbeddedFactory) error {
	if c == nil || factory == nil {
		return errors.New("内嵌插件工厂不能为空")
	}
	definition := factory()
	if strings.TrimSpace(definition.ID) == "" || strings.TrimSpace(definition.Version) == "" {
		return errors.New("内嵌插件工厂必须提供身份和版本")
	}
	key := embeddedCatalogKey(definition.ID, definition.Version)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = map[string]embeddedCatalogEntry{}
	}
	if _, exists := c.entries[key]; exists {
		return fmt.Errorf("内嵌插件目录重复: %s@%s", definition.ID, definition.Version)
	}
	c.entries[key] = embeddedCatalogEntry{id: definition.ID, version: definition.Version, factory: factory}
	return nil
}

func (c *EmbeddedCatalog) Resolve(id, version string) (protocolbus.EmbeddedPlugin, bool, error) {
	if c == nil {
		return protocolbus.EmbeddedPlugin{}, false, nil
	}
	c.mu.RLock()
	entry, ok := c.entries[embeddedCatalogKey(id, version)]
	c.mu.RUnlock()
	if !ok {
		return protocolbus.EmbeddedPlugin{}, false, nil
	}
	definition := entry.factory()
	if definition.ID != entry.id || definition.Version != entry.version {
		return protocolbus.EmbeddedPlugin{}, false, fmt.Errorf("内嵌插件工厂身份漂移: 登记 %s@%s，返回 %s@%s",
			entry.id, entry.version, definition.ID, definition.Version)
	}
	return definition, true, nil
}

type PlacementMode string

const (
	PlacementProcessOnly      PlacementMode = "process-only"
	PlacementPreferEmbedded   PlacementMode = "prefer-embedded"
	PlacementRequireEmbedded  PlacementMode = "require-embedded"
	PlacementPreferDynamicGo  PlacementMode = "prefer-dynamic-go"
	PlacementRequireDynamicGo PlacementMode = "require-dynamic-go"
)

func validatePlacementMode(mode PlacementMode) error {
	switch mode {
	case PlacementProcessOnly, PlacementPreferEmbedded, PlacementRequireEmbedded,
		PlacementPreferDynamicGo, PlacementRequireDynamicGo:
		return nil
	default:
		return fmt.Errorf("插件放置策略无效: %q（可选: %s, %s, %s, %s, %s）", mode,
			PlacementProcessOnly, PlacementPreferEmbedded, PlacementRequireEmbedded,
			PlacementPreferDynamicGo, PlacementRequireDynamicGo)
	}
}

// PlacementPolicy 只决定进程/内嵌形态，不代替 ExecutionPolicy 的发布者信任和
// minimumIsolation 检查。精确插件规则优先于发布者规则，发布者规则优先于全局规则。
type PlacementPolicy struct {
	Default           PlacementMode
	PublisherPolicies map[string]PlacementMode
	PluginPolicies    map[string]PlacementMode
}

func ParsePlacementPolicy(defaultMode, publisherRules, pluginRules string) (PlacementPolicy, error) {
	policy := PlacementPolicy{
		Default:           PlacementMode(strings.TrimSpace(defaultMode)),
		PublisherPolicies: map[string]PlacementMode{}, PluginPolicies: map[string]PlacementMode{},
	}
	if err := validatePlacementMode(policy.Default); err != nil {
		return PlacementPolicy{}, err
	}
	if err := parsePlacementRules("发布者", publisherRules, policy.PublisherPolicies); err != nil {
		return PlacementPolicy{}, err
	}
	if err := parsePlacementRules("插件", pluginRules, policy.PluginPolicies); err != nil {
		return PlacementPolicy{}, err
	}
	return policy, nil
}

func parsePlacementRules(kind, raw string, target map[string]PlacementMode) error {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	for _, rawRule := range strings.Split(raw, ",") {
		name, value, ok := strings.Cut(rawRule, "=")
		name, mode := strings.TrimSpace(name), PlacementMode(strings.TrimSpace(value))
		if !ok || name == "" || mode == "" {
			return fmt.Errorf("%s放置策略格式无效: %q（应为 name=mode）", kind, rawRule)
		}
		if _, exists := target[name]; exists {
			return fmt.Errorf("%s放置策略重复: %s", kind, name)
		}
		if err := validatePlacementMode(mode); err != nil {
			return fmt.Errorf("%s %s: %w", kind, name, err)
		}
		target[name] = mode
	}
	return nil
}

func (p PlacementPolicy) modeFor(plugin InstalledPlugin) PlacementMode {
	if mode, ok := p.PluginPolicies[plugin.ID]; ok {
		return mode
	}
	if mode, ok := p.PublisherPolicies[plugin.Publisher]; ok {
		return mode
	}
	if p.Default == "" {
		return PlacementProcessOnly
	}
	return p.Default
}

func (r *ProtocolRuntime) startPlugin(ctx context.Context, host *protocolbus.Host, plugin InstalledPlugin,
	policy protocolbus.LaunchPolicy) (*protocolbus.PluginProcess, error) {
	mode := r.PlacementPolicy.modeFor(plugin)
	if err := validatePlacementMode(mode); err != nil {
		return nil, err
	}
	if mode == PlacementProcessOnly {
		return r.startProcessPlugin(ctx, host, plugin, policy)
	}
	required := mode == PlacementRequireEmbedded || mode == PlacementRequireDynamicGo
	fallbackProcess := func(reason error) (*protocolbus.PluginProcess, error) {
		if required {
			return nil, reason
		}
		if r.Logf != nil {
			r.Logf("插件 %s@%s 无法以内嵌形态启动，回退独立进程: %v", plugin.ID, plugin.Version, reason)
		}
		return r.startProcessPlugin(ctx, host, plugin, policy)
	}
	if err := validateInProcessFirstParty(plugin); err != nil {
		if required {
			return nil, err
		}
		return r.startProcessPlugin(ctx, host, plugin, policy)
	}
	candidate := plugin
	if strings.TrimSpace(candidate.Execution.MinimumIsolation) == "" {
		candidate.Execution.MinimumIsolation = string(IsolationTrustedProcess)
	}
	minimum, isolationErr := r.ExecutionPolicy.RequiredIsolation(candidate)
	if isolationErr != nil || isolationRank[minimum] > isolationRank[IsolationTrustedProcess] {
		if required {
			if isolationErr != nil {
				return nil, isolationErr
			}
			return nil, fmt.Errorf("插件 %s 要求隔离 %s，不能放入内核进程", plugin.ID, minimum)
		}
		return r.startProcessPlugin(ctx, host, plugin, policy)
	}

	if mode != PlacementPreferDynamicGo && mode != PlacementRequireDynamicGo {
		definition, found, err := r.EmbeddedCatalog.Resolve(plugin.ID, plugin.Version)
		if err != nil {
			return nil, err
		}
		if found {
			return host.LaunchEmbeddedWithPolicy(ctx, definition, policy)
		}
	}
	if plugin.DynamicGoPath != "" {
		if plugin.Execution.DynamicGo == nil || plugin.Execution.DynamicGo.ABI != protocolbus.DynamicGoABIV1 {
			return fallbackProcess(errors.New("dynamic-go 安装契约缺失或 ABI 无效"))
		}
		if r.DynamicGoLoader == nil {
			return fallbackProcess(errors.New("Backend 未配置 dynamic-go loader"))
		}
		definition, err := r.DynamicGoLoader.Load(plugin.DynamicGoPath, plugin.ID, plugin.Version,
			plugin.Execution.DynamicGo.Fingerprint)
		if err != nil {
			return fallbackProcess(err)
		}
		return host.LaunchEmbeddedWithPolicy(ctx, definition, policy)
	}
	if required {
		if mode == PlacementRequireDynamicGo {
			return nil, fmt.Errorf("插件 %s@%s 没有已验签的 dynamic-go 入口", plugin.ID, plugin.Version)
		}
		return nil, fmt.Errorf("插件 %s@%s 既不在静态目录中，也没有 dynamic-go 入口", plugin.ID, plugin.Version)
	}
	return r.startProcessPlugin(ctx, host, plugin, policy)
}

func (r *ProtocolRuntime) startProcessPlugin(ctx context.Context, host *protocolbus.Host, plugin InstalledPlugin,
	policy protocolbus.LaunchPolicy) (*protocolbus.PluginProcess, error) {
	spec, err := r.launchSpec(plugin)
	if err != nil {
		return nil, err
	}
	return host.LaunchSpecWithPolicy(ctx, spec, policy)
}
