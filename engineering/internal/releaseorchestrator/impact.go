package releaseorchestrator

import (
	"fmt"
	"sort"
	"strings"

	semver "github.com/Masterminds/semver/v3"
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
	PluginID          string             `json:"pluginId"`
	Change            ReleaseChangeClass `json:"change"`
	ReusedConsumers   []string           `json:"reusedConsumers,omitempty"`
	RequiredConsumers []string           `json:"requiredConsumers,omitempty"`
}

// AnalyzeReleaseImpact keeps upgrade closure minimal. Dependencies are a
// compatibility contract: implementation and additive releases reuse all
// consumers whose declared range accepts the new producer version. A publisher
// must explicitly mark a breaking public-surface change; then every direct
// consumer must join the release selection so its new compatibility assertion
// and tests are part of the same candidate generation.
func AnalyzeReleaseImpact(workspace PluginWorkspace, requests map[string]ReleasePluginRequest) ([]ReleaseImpact, error) {
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
		impact := ReleaseImpact{PluginID: id, Change: change}
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
