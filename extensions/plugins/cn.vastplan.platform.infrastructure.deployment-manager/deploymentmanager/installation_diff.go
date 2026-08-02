package deploymentmanager

import (
	"reflect"
	"sort"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/plugininstallation"
)

func diffArtifactLocks(before, after *pluginv1.ArtifactLock) []plugininstallation.PackageChange {
	beforePackages, afterPackages := lockPackageMap(before), lockPackageMap(after)
	rootIDs := lockRootSet(before, after)
	ids := make(map[string]struct{}, len(beforePackages)+len(afterPackages))
	for id := range beforePackages {
		ids[id] = struct{}{}
	}
	for id := range afterPackages {
		ids[id] = struct{}{}
	}
	ordered := make([]string, 0, len(ids))
	for id := range ids {
		ordered = append(ordered, id)
	}
	sort.Strings(ordered)
	changes := make([]plugininstallation.PackageChange, 0)
	for _, id := range ordered {
		beforePackage, hadBefore := beforePackages[id]
		afterPackage, hasAfter := afterPackages[id]
		kind := plugininstallation.PackageChangeKind("")
		switch {
		case !hadBefore && hasAfter:
			kind = plugininstallation.PackageAdded
		case hadBefore && !hasAfter:
			kind = plugininstallation.PackageRemoved
		case hadBefore && hasAfter && !reflect.DeepEqual(beforePackage, afterPackage):
			kind = plugininstallation.PackageUpdated
		}
		if kind == "" {
			continue
		}
		change := plugininstallation.PackageChange{Kind: kind, PluginID: id, Root: rootIDs[id]}
		if hadBefore {
			value := cloneJSON(beforePackage)
			change.Before = &value
		}
		if hasAfter {
			value := cloneJSON(afterPackage)
			change.After = &value
		}
		changes = append(changes, change)
	}
	return changes
}

func lockPackageMap(lock *pluginv1.ArtifactLock) map[string]pluginv1.ArtifactLockPackage {
	result := map[string]pluginv1.ArtifactLockPackage{}
	if lock == nil {
		return result
	}
	for _, item := range lock.Packages {
		result[item.Ref.PluginID] = item
	}
	return result
}

func lockRootSet(locks ...*pluginv1.ArtifactLock) map[string]bool {
	result := map[string]bool{}
	for _, lock := range locks {
		if lock == nil {
			continue
		}
		for _, root := range lock.Roots {
			result[root.PluginID] = true
		}
	}
	return result
}

func cloneArtifactLock(lock *pluginv1.ArtifactLock) *pluginv1.ArtifactLock {
	if lock == nil {
		return nil
	}
	value := cloneJSON(*lock)
	return &value
}
