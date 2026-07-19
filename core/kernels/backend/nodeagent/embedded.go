package nodeagent

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"cdsoft.com.cn/VastPlan/core/shared/go/protocolbus"
)

type PlacementMode string

const (
	PlacementProcessOnly      PlacementMode = "process-only"
	PlacementPreferDynamicGo  PlacementMode = "prefer-dynamic-go"
	PlacementRequireDynamicGo PlacementMode = "require-dynamic-go"
)

func validatePlacementMode(mode PlacementMode) error {
	switch mode {
	case PlacementProcessOnly, PlacementPreferDynamicGo, PlacementRequireDynamicGo:
		return nil
	default:
		return fmt.Errorf("插件放置策略无效: %q（可选: %s, %s, %s）", mode,
			PlacementProcessOnly, PlacementPreferDynamicGo, PlacementRequireDynamicGo)
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

// DynamicGoExecutionDriver 把历史 dynamic-go 承载纳入统一执行驱动入口。
// 选择与可回退策略仍由宿主 PlacementPolicy 决定，驱动本身不拥有提权能力。
type DynamicGoExecutionDriver struct{ Loader DynamicGoModuleLoader }

func (DynamicGoExecutionDriver) Name() string              { return "dynamic-go" }
func (DynamicGoExecutionDriver) Isolation() IsolationLevel { return IsolationTrustedRuntime }
func (d DynamicGoExecutionDriver) Start(ctx context.Context, host *protocolbus.Host, plugin InstalledPlugin,
	policy protocolbus.LaunchPolicy) (*protocolbus.PluginInstance, error) {
	if err := validateInProcessFirstParty(plugin); err != nil {
		return nil, err
	}
	if plugin.DynamicGoPath == "" {
		return nil, fmt.Errorf("插件 %s@%s 没有已验签的 dynamic-go 入口", plugin.ID, plugin.Version)
	}
	if plugin.Execution.DynamicGo == nil || plugin.Execution.DynamicGo.ABI != protocolbus.DynamicGoABIV1 {
		return nil, errors.New("dynamic-go 安装契约缺失或 ABI 无效")
	}
	if d.Loader == nil {
		return nil, errors.New("Backend 未配置 dynamic-go loader")
	}
	definition, err := d.Loader.Load(plugin.DynamicGoPath, plugin.ID, plugin.Version,
		plugin.Execution.DynamicGo.Fingerprint)
	if err != nil {
		return nil, err
	}
	return host.LaunchEmbeddedKindWithPolicy(ctx, definition, policy, d.Name())
}

func (r *ProtocolRuntime) startPlugin(ctx context.Context, host *protocolbus.Host, plugin InstalledPlugin,
	policy protocolbus.LaunchPolicy) (*protocolbus.PluginInstance, error) {
	mode := r.PlacementPolicy.modeFor(plugin)
	if err := validatePlacementMode(mode); err != nil {
		return nil, err
	}
	if plugin.Execution.DynamicGo != nil && plugin.Execution.DynamicGo.Required && mode != PlacementRequireDynamicGo {
		return nil, fmt.Errorf("插件 %s@%s 的签名执行契约要求 require-dynamic-go，实际为 %s",
			plugin.ID, plugin.Version, mode)
	}
	if mode == PlacementProcessOnly {
		return r.startConfiguredPlugin(ctx, host, plugin, policy)
	}
	required := mode == PlacementRequireDynamicGo
	fallbackConfigured := func(reason error) (*protocolbus.PluginInstance, error) {
		if required {
			return nil, reason
		}
		if r.Logf != nil {
			r.Logf("插件 %s@%s 无法以 dynamic-go 启动，回退签名清单驱动 %s: %v",
				plugin.ID, plugin.Version, plugin.Execution.Driver, reason)
		}
		return r.startConfiguredPlugin(ctx, host, plugin, policy)
	}
	if err := validateInProcessFirstParty(plugin); err != nil {
		if required {
			return nil, err
		}
		return r.startConfiguredPlugin(ctx, host, plugin, policy)
	}
	candidate := normalizeExecutionContract(plugin)
	minimum, isolationErr := r.ExecutionPolicy.RequiredIsolation(candidate)
	driver := DynamicGoExecutionDriver{Loader: r.DynamicGoLoader}
	if isolationErr != nil || isolationRank[driver.Isolation()] < isolationRank[minimum] {
		if required {
			if isolationErr != nil {
				return nil, isolationErr
			}
			return nil, fmt.Errorf("插件 %s 要求隔离 %s，不能放入内核进程", plugin.ID, minimum)
		}
		return r.startConfiguredPlugin(ctx, host, plugin, policy)
	}
	instance, err := driver.Start(ctx, host, candidate, policy)
	if err != nil {
		return fallbackConfigured(err)
	}
	return instance, nil
}
