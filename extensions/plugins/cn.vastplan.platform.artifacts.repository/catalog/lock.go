package catalog

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	semver "github.com/Masterminds/semver/v3"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

func buildLock(revision uint64, request pluginv1.ArtifactResolveRequest, selected map[string]Entry) (pluginv1.ArtifactLock, error) {
	roots := make([]pluginv1.ArtifactRequirement, len(request.Roots))
	for index, root := range request.Roots {
		root.Features = append([]string(nil), root.Features...)
		roots[index] = root
	}
	sort.Slice(roots, func(i, j int) bool { return roots[i].PluginID < roots[j].PluginID })
	packages := make([]pluginv1.ArtifactLockPackage, 0, len(selected))
	for _, entry := range selected {
		packages = append(packages, pluginv1.ArtifactLockPackage{
			Ref: entry.Ref, SHA256: entry.SHA256, Size: entry.Size, Publisher: entry.Publisher,
			KeyID: entry.KeyID, RepositoryRevision: entry.RepositoryRevision,
			Dependencies:    cloneStringMap(entry.Dependencies),
			LifecycleStatus: deprecatedStatus(entry), LifecycleReason: deprecatedReason(entry), Replacement: deprecatedReplacement(entry),
		})
	}
	sort.Slice(packages, func(i, j int) bool { return packages[i].Ref.PluginID < packages[j].Ref.PluginID })
	lock := pluginv1.ArtifactLock{
		SchemaVersion: artifactLockSchemaVersion, RepositoryRevision: revision,
		Target: request.Target, KernelVersion: request.KernelVersion, Platform: request.Platform,
		Roots: roots, Packages: packages,
	}
	digest, err := artifactLockDigest(lock)
	if err != nil {
		return pluginv1.ArtifactLock{}, err
	}
	lock.Digest = digest
	raw, err := json.Marshal(lock)
	if err != nil {
		return pluginv1.ArtifactLock{}, err
	}
	if err := pluginv1.ValidateArtifactLock(raw); err != nil {
		return pluginv1.ArtifactLock{}, err
	}
	return lock, nil
}

func deprecatedStatus(entry Entry) string {
	if entry.LifecycleStatus == LifecycleDeprecated {
		return LifecycleDeprecated
	}
	return ""
}

func deprecatedReason(entry Entry) string {
	if entry.LifecycleStatus == LifecycleDeprecated {
		return entry.LifecycleReason
	}
	return ""
}

func deprecatedReplacement(entry Entry) *pluginv1.ArtifactRequirement {
	if entry.LifecycleStatus == LifecycleDeprecated {
		return cloneRequirement(entry.Replacement)
	}
	return nil
}

func ValidateLock(lock pluginv1.ArtifactLock) error {
	raw, err := json.Marshal(lock)
	if err != nil {
		return err
	}
	if err := pluginv1.ValidateArtifactLock(raw); err != nil {
		return err
	}
	digest, err := artifactLockDigest(lock)
	if err != nil {
		return err
	}
	if digest != lock.Digest {
		return errors.New("制品锁 digest 与规范内容不一致")
	}
	seen := map[string]struct{}{}
	locked := map[string]pluginv1.ArtifactLockPackage{}
	previous := ""
	for _, item := range lock.Packages {
		if _, duplicate := seen[item.Ref.PluginID]; duplicate {
			return fmt.Errorf("制品锁包含重复插件 ID: %s", item.Ref.PluginID)
		}
		if previous != "" && item.Ref.PluginID <= previous {
			return errors.New("制品锁 packages 必须按 pluginId 严格升序排列")
		}
		if item.RepositoryRevision > lock.RepositoryRevision {
			return fmt.Errorf("制品 %s 晚于锁定的 Catalog revision", item.Ref.PluginID)
		}
		seen[item.Ref.PluginID] = struct{}{}
		locked[item.Ref.PluginID] = item
		previous = item.Ref.PluginID
	}
	for _, item := range lock.Packages {
		for dependency, rawConstraint := range item.Dependencies {
			selected, ok := locked[dependency]
			if !ok {
				return fmt.Errorf("制品锁缺少依赖 %s -> %s", item.Ref.PluginID, dependency)
			}
			constraint, err := semver.NewConstraint(rawConstraint)
			version, versionErr := semver.NewVersion(selected.Ref.Version)
			if err != nil || versionErr != nil || !constraint.Check(version) {
				return fmt.Errorf("制品锁依赖不满足 %s -> %s %s", item.Ref.PluginID, dependency, rawConstraint)
			}
		}
	}
	rootPrevious := ""
	for _, root := range lock.Roots {
		if rootPrevious != "" && root.PluginID <= rootPrevious {
			return errors.New("制品锁 roots 必须按 pluginId 严格升序排列")
		}
		normalized, normalizeErr := pluginv1.NormalizeArtifactRequirement(root)
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
	entries := make(map[string]Entry, len(lock.Packages))
	for _, item := range lock.Packages {
		entries[item.Ref.PluginID] = Entry{Ref: item.Ref, Dependencies: item.Dependencies}
	}
	if cycle := dependencyCycle(entries); len(cycle) > 0 {
		return errors.New("制品锁包含依赖环: " + strings.Join(cycle, " -> "))
	}
	return nil
}

func artifactLockDigest(lock pluginv1.ArtifactLock) (string, error) {
	return pluginv1.ArtifactLockDigest(lock)
}

func sortCandidates(entries []Entry, channelRank map[string]int) {
	sort.Slice(entries, func(i, j int) bool {
		left, leftErr := semver.NewVersion(entries[i].Ref.Version)
		right, rightErr := semver.NewVersion(entries[j].Ref.Version)
		if leftErr == nil && rightErr == nil && !left.Equal(right) {
			return left.GreaterThan(right)
		}
		if entries[i].Ref.Version != entries[j].Ref.Version {
			return entries[i].Ref.Version > entries[j].Ref.Version
		}
		if channelRank[entries[i].Ref.Channel] != channelRank[entries[j].Ref.Channel] {
			return channelRank[entries[i].Ref.Channel] < channelRank[entries[j].Ref.Channel]
		}
		return entries[i].RepositoryRevision > entries[j].RepositoryRevision
	})
}

func cloneEntry(entry Entry) Entry {
	entry.Engines = cloneStringMap(entry.Engines)
	entry.Dependencies = cloneStringMap(entry.Dependencies)
	entry.CompositionFeatures = cloneCompositionFeatures(entry.CompositionFeatures)
	entry.Targets = append([]string(nil), entry.Targets...)
	entry.Platforms = append([]string(nil), entry.Platforms...)
	entry.RuntimeRequires = append([]pluginv1.RuntimeRequirement(nil), entry.RuntimeRequires...)
	entry.RuntimeProvides = append([]pluginv1.RuntimeCapabilityPolicy(nil), entry.RuntimeProvides...)
	entry.ProvidedCapabilities = append([]string(nil), entry.ProvidedCapabilities...)
	return entry
}

func cloneCompositionFeatures(source map[string]pluginv1.CompositionFeature) map[string]pluginv1.CompositionFeature {
	if len(source) == 0 {
		return nil
	}
	out := make(map[string]pluginv1.CompositionFeature, len(source))
	for id, feature := range source {
		feature.Dependencies = cloneStringMap(feature.Dependencies)
		feature.RuntimeRequires = append([]pluginv1.RuntimeRequirement(nil), feature.RuntimeRequires...)
		feature.ConfigurationSchema = append([]byte(nil), feature.ConfigurationSchema...)
		out[id] = feature
	}
	return out
}

func cloneStringMap(source map[string]string) map[string]string {
	if len(source) == 0 {
		return nil
	}
	out := make(map[string]string, len(source))
	for key, value := range source {
		out[key] = value
	}
	return out
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func resolutionError(code, message string) error {
	return &ResolutionError{Code: code, Message: message}
}
