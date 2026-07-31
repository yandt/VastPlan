package pluginv1

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	semver "github.com/Masterminds/semver/v3"
)

// ValidateArtifactLockSemantics validates the closed dependency graph carried
// by an Artifact Lock. Schema validation alone cannot prove that roots and
// transitive dependencies are satisfied, so every repository, installer and
// kernel consumer must call this shared semantic validator.
func ValidateArtifactLockSemantics(lock ArtifactLock) error {
	raw, err := json.Marshal(lock)
	if err != nil {
		return err
	}
	if err := ValidateArtifactLock(raw); err != nil {
		return err
	}
	digest, err := ArtifactLockDigest(lock)
	if err != nil {
		return err
	}
	if digest != lock.Digest {
		return errors.New("制品锁 digest 与规范内容不一致")
	}

	locked := make(map[string]ArtifactLockPackage, len(lock.Packages))
	previous := ""
	for _, item := range lock.Packages {
		if _, duplicate := locked[item.Ref.PluginID]; duplicate {
			return fmt.Errorf("制品锁包含重复插件 ID: %s", item.Ref.PluginID)
		}
		if previous != "" && item.Ref.PluginID <= previous {
			return errors.New("制品锁 packages 必须按 pluginId 严格升序排列")
		}
		if item.RepositoryRevision > lock.RepositoryRevision {
			return fmt.Errorf("制品 %s 晚于锁定的 Catalog revision", item.Ref.PluginID)
		}
		locked[item.Ref.PluginID] = item
		previous = item.Ref.PluginID
	}

	for _, item := range lock.Packages {
		for dependency, rawConstraint := range item.Dependencies {
			selected, ok := locked[dependency]
			if !ok {
				return fmt.Errorf("制品锁缺少依赖 %s -> %s", item.Ref.PluginID, dependency)
			}
			constraint, constraintErr := semver.NewConstraint(rawConstraint)
			version, versionErr := semver.NewVersion(selected.Ref.Version)
			if constraintErr != nil || versionErr != nil || !constraint.Check(version) {
				return fmt.Errorf("制品锁依赖不满足 %s -> %s %s", item.Ref.PluginID, dependency, rawConstraint)
			}
		}
	}

	rootPrevious := ""
	for _, root := range lock.Roots {
		if rootPrevious != "" && root.PluginID <= rootPrevious {
			return errors.New("制品锁 roots 必须按 pluginId 严格升序排列")
		}
		normalized, normalizeErr := NormalizeArtifactRequirement(root)
		if normalizeErr != nil || normalized.PluginID != root.PluginID || normalized.Constraint != root.Constraint || normalized.Channel != root.Channel || !slices.Equal(normalized.Features, root.Features) {
			return fmt.Errorf("制品锁根 Requirement 未规范化: %s", root.PluginID)
		}
		selected, ok := locked[root.PluginID]
		constraint, constraintErr := semver.NewConstraint(root.Constraint)
		version, versionErr := semver.NewVersion(selected.Ref.Version)
		if !ok || constraintErr != nil || versionErr != nil || !constraint.Check(version) || root.Channel != "" && selected.Ref.Channel != root.Channel {
			return fmt.Errorf("制品锁根依赖不满足: %s %s", root.PluginID, root.Constraint)
		}
		rootPrevious = root.PluginID
	}
	if cycle := artifactLockDependencyCycle(locked); len(cycle) > 0 {
		return errors.New("制品锁包含依赖环: " + strings.Join(cycle, " -> "))
	}
	return nil
}

func artifactLockDependencyCycle(packages map[string]ArtifactLockPackage) []string {
	const (
		unvisited = iota
		visiting
		visited
	)
	states := make(map[string]int, len(packages))
	stack := make([]string, 0, len(packages))
	positions := make(map[string]int, len(packages))
	ids := make([]string, 0, len(packages))
	for id := range packages {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var visit func(string) []string
	visit = func(id string) []string {
		if states[id] == visiting {
			start := positions[id]
			return append(append([]string(nil), stack[start:]...), id)
		}
		if states[id] == visited {
			return nil
		}
		states[id] = visiting
		positions[id] = len(stack)
		stack = append(stack, id)
		dependencies := make([]string, 0, len(packages[id].Dependencies))
		for dependency := range packages[id].Dependencies {
			dependencies = append(dependencies, dependency)
		}
		sort.Strings(dependencies)
		for _, dependency := range dependencies {
			if cycle := visit(dependency); len(cycle) > 0 {
				return cycle
			}
		}
		stack = stack[:len(stack)-1]
		delete(positions, id)
		states[id] = visited
		return nil
	}
	for _, id := range ids {
		if cycle := visit(id); len(cycle) > 0 {
			return cycle
		}
	}
	return nil
}
