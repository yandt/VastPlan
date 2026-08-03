package releaseorchestrator

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	semver "github.com/Masterminds/semver/v3"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

// ReleaseChangeClass is the publisher's compatibility assertion for this
// release. It is intentionally about the signed public surface, not about
// implementation bytes: every changed package gets a new immutable version,
// but compatible consumers do not need a new package or generation.
type ReleaseChangeClass string

const (
	ReleaseChangeImplementation ReleaseChangeClass = "implementation"
	ReleaseChangeAdditive       ReleaseChangeClass = "additive"
	ReleaseChangeBreaking       ReleaseChangeClass = "breaking"
)

type ReleaseImpact struct {
	PluginID                     string                         `json:"pluginId"`
	Change                       ReleaseChangeClass             `json:"change"`
	InterfaceFingerprint         string                         `json:"interfaceFingerprint"`
	BaselineInterfaceFingerprint string                         `json:"baselineInterfaceFingerprint,omitempty"`
	InterfaceChange              pluginv1.PublicInterfaceChange `json:"interfaceChange,omitempty"`
	ReusedConsumers              []string                       `json:"reusedConsumers,omitempty"`
	RequiredConsumers            []string                       `json:"requiredConsumers,omitempty"`
}

// AnalyzeReleaseImpact keeps upgrade closure minimal. Dependencies are a
// compatibility contract: implementation and additive releases reuse all
// consumers whose declared range accepts the new producer version. A publisher
// must explicitly mark a breaking public-surface change; then every direct
// consumer must join the release selection so its new compatibility assertion
// and tests are part of the same candidate generation.
func AnalyzeReleaseImpact(workspace PluginWorkspace, requests map[string]ReleasePluginRequest) ([]ReleaseImpact, error) {
	return AnalyzeReleaseImpactWithBaseline(workspace, requests, nil)
}

// AnalyzeReleaseImpactWithBaseline cross-checks the publisher's declared
// change class against the active generation exactly once, before dependency
// closure is calculated. A missing development baseline remains observable as
// an unverified comparison; production callers must inject one.
func AnalyzeReleaseImpactWithBaseline(workspace PluginWorkspace, requests map[string]ReleasePluginRequest, baseline *InterfaceBaseline) ([]ReleaseImpact, error) {
	if baseline != nil {
		if err := pluginv1.ValidatePluginInventory(baseline.Inventory); err != nil {
			return nil, fmt.Errorf("活动 Plugin Inventory 基线无效: %w", err)
		}
	}
	ids := make([]string, 0, len(requests))
	for id := range requests {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	impacts := make([]ReleaseImpact, 0, len(ids))
	for _, id := range ids {
		plugin, exists := workspace.Plugins[id]
		if !exists {
			return nil, fmt.Errorf("影响分析引用了不存在的插件 %s", id)
		}
		change := requests[id].Change
		fingerprint, err := pluginv1.PublicInterfaceFingerprint(plugin.Manifest)
		if err != nil {
			return nil, fmt.Errorf("计算 %s 的公共接口指纹: %w", id, err)
		}
		impact := ReleaseImpact{PluginID: id, Change: change, InterfaceFingerprint: fingerprint}
		if err := verifyDeclaredInterfaceChange(&impact, plugin.Manifest, baseline); err != nil {
			return nil, fmt.Errorf("校验插件 %s 的 %s 变更: %w", id, change, err)
		}
		for _, consumerID := range workspace.ReverseDependencies(id) {
			if _, selected := requests[consumerID]; selected {
				continue
			}
			consumer := workspace.Plugins[consumerID]
			constraint, depends := allDependencies(consumer.Manifest)[id]
			if !depends {
				continue
			}
			// Workspace loading already validates this range. Keep the explicit
			// check here so the impact record remains defensible if callers build
			// a workspace from a historical catalog rather than local sources.
			if err := acceptsVersion(constraint, plugin.Version); err != nil {
				return nil, fmt.Errorf("分析 %s 对 %s 的依赖: %w", consumerID, id, err)
			}
			if change == ReleaseChangeBreaking {
				impact.RequiredConsumers = append(impact.RequiredConsumers, consumerID)
			} else {
				impact.ReusedConsumers = append(impact.ReusedConsumers, consumerID)
			}
		}
		if len(impact.RequiredConsumers) > 0 {
			return nil, fmt.Errorf("插件 %s 声明 breaking 变更，必须同时选择直接消费者: %s", id, strings.Join(impact.RequiredConsumers, ", "))
		}
		impacts = append(impacts, impact)
	}
	return impacts, nil
}

func verifyDeclaredInterfaceChange(impact *ReleaseImpact, candidate pluginv1.Manifest, baseline *InterfaceBaseline) error {
	if baseline == nil {
		impact.InterfaceChange = "unverified"
		return nil
	}
	previous, exists, err := baselinePlugin(baseline.Inventory, impact.PluginID)
	if err != nil {
		return err
	}
	if !exists {
		impact.InterfaceChange = "initial"
		return nil
	}
	impact.BaselineInterfaceFingerprint = previous.InterfaceFingerprint
	if previous.InterfaceFingerprint == "" {
		return errors.New("活动 Inventory 缺少公开接口指纹，不能安全判定升级")
	}
	if impact.Change == ReleaseChangeImplementation {
		if previous.InterfaceFingerprint != impact.InterfaceFingerprint {
			return fmt.Errorf("implementation 声明要求公开接口不变: active=%s candidate=%s；请改为 additive 或 breaking", previous.InterfaceFingerprint, impact.InterfaceFingerprint)
		}
		impact.InterfaceChange = pluginv1.PublicInterfaceUnchanged
		return nil
	}
	if impact.Change == ReleaseChangeBreaking {
		if len(previous.PublicInterface) == 0 {
			impact.InterfaceChange = "unverified"
			return nil
		}
		candidateSurface, err := pluginv1.PublicInterfaceSurface(candidate)
		if err != nil {
			return err
		}
		observed, err := pluginv1.ComparePublicInterfaceSurfaces(previous.PublicInterface, candidateSurface)
		if err != nil {
			return err
		}
		impact.InterfaceChange = observed
		return nil
	}
	if len(previous.PublicInterface) == 0 {
		return errors.New("活动 Inventory 缺少公开接口描述，不能证明 additive 兼容性；请按 breaking 发布或先重建基线")
	}
	candidateSurface, err := pluginv1.PublicInterfaceSurface(candidate)
	if err != nil {
		return err
	}
	change, err := pluginv1.ComparePublicInterfaceSurfaces(previous.PublicInterface, candidateSurface)
	if err != nil {
		return err
	}
	if change == pluginv1.PublicInterfaceBreaking {
		return errors.New("additive 声明修改或删除了既有公开接口；请改为 breaking 并同时选择直接消费者")
	}
	impact.InterfaceChange = change
	return nil
}

func baselinePlugin(inventory pluginv1.PluginInventorySnapshot, pluginID string) (pluginv1.PluginInventoryItem, bool, error) {
	var found *pluginv1.PluginInventoryItem
	for index := range inventory.Plugins {
		item := &inventory.Plugins[index]
		if item.Artifact.Ref.PluginID != pluginID {
			continue
		}
		if found != nil {
			return pluginv1.PluginInventoryItem{}, false, fmt.Errorf("活动 Inventory 对插件 %s 包含多个精确制品，必须注入已激活 Selection 的 Inventory", pluginID)
		}
		found = item
	}
	if found == nil {
		return pluginv1.PluginInventoryItem{}, false, nil
	}
	return *found, true, nil
}

func acceptsVersion(constraintText, version string) error {
	constraint, err := semver.NewConstraint(constraintText)
	if err != nil {
		return err
	}
	parsed, err := semver.StrictNewVersion(version)
	if err != nil {
		return err
	}
	if !constraint.Check(parsed) {
		return fmt.Errorf("约束 %s 不接受版本 %s", constraintText, version)
	}
	return nil
}
