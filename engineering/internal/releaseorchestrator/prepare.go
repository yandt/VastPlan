package releaseorchestrator

import (
	"fmt"
	"sort"
)

func PrepareRelease(repositoryRoot string, spec ReleaseSpec) (ReleasePlan, error) {
	workspace, err := LoadPluginWorkspace(repositoryRoot)
	if err != nil {
		return ReleasePlan{}, err
	}
	versions := make(map[string]string, len(spec.Plugins))
	for _, request := range spec.Plugins {
		plugin, ok := workspace.Plugins[request.ID]
		if !ok {
			return ReleasePlan{}, fmt.Errorf("Release Spec 引用了不存在的插件 %s", request.ID)
		}
		versions[request.ID] = plugin.Version
	}
	contractChanges, err := SyncContracts(repositoryRoot, true)
	if err != nil {
		return ReleasePlan{}, err
	}
	capabilityChanges, err := SyncCapabilityContractProjections(repositoryRoot, workspace, true)
	if err != nil {
		return ReleasePlan{}, err
	}
	packageVersionChanges, err := SyncSelectedPluginPackageVersions(repositoryRoot, workspace, versions, true)
	if err != nil {
		return ReleasePlan{}, err
	}
	runtimeVersionChanges, err := SyncSelectedPluginRuntimeVersions(repositoryRoot, workspace, versions, true)
	if err != nil {
		return ReleasePlan{}, err
	}
	deploymentChanges, err := SyncDeploymentReferences(repositoryRoot, versions)
	if err != nil {
		return ReleasePlan{}, err
	}
	if changes, err := SyncContracts(repositoryRoot, false); err != nil {
		return ReleasePlan{}, err
	} else if len(changes) != 0 {
		return ReleasePlan{}, fmt.Errorf("Contract Registry 生成结果未收敛: changes=%v", changes)
	}
	if changes, err := SyncCapabilityContractProjections(repositoryRoot, workspace, false); err != nil {
		return ReleasePlan{}, err
	} else if len(changes) != 0 {
		return ReleasePlan{}, fmt.Errorf("Capability Contract 投影未收敛: changes=%v", changes)
	}
	if changes, err := SyncSelectedPluginPackageVersions(repositoryRoot, workspace, versions, false); err != nil {
		return ReleasePlan{}, err
	} else if len(changes) != 0 {
		return ReleasePlan{}, fmt.Errorf("package version 投影未收敛: changes=%v", changes)
	}
	if changes, err := SyncSelectedPluginRuntimeVersions(repositoryRoot, workspace, versions, false); err != nil {
		return ReleasePlan{}, err
	} else if len(changes) != 0 {
		return ReleasePlan{}, fmt.Errorf("runtime version 投影未收敛: changes=%v", changes)
	}
	plan, err := BuildReleasePlan(repositoryRoot, spec)
	if err != nil {
		return ReleasePlan{}, err
	}
	plan.DeploymentChanges = deploymentChanges
	generated := make(map[string]struct{}, len(plan.GeneratedFiles)+len(contractChanges)+len(capabilityChanges)+len(packageVersionChanges))
	for _, path := range plan.GeneratedFiles {
		generated[path] = struct{}{}
	}
	derivedChanges := make([]DerivedChange, 0, len(contractChanges)+len(capabilityChanges)+len(packageVersionChanges)+len(runtimeVersionChanges))
	derivedChanges = append(derivedChanges, contractChanges...)
	derivedChanges = append(derivedChanges, capabilityChanges...)
	derivedChanges = append(derivedChanges, packageVersionChanges...)
	derivedChanges = append(derivedChanges, runtimeVersionChanges...)
	for _, change := range derivedChanges {
		generated[change.Path] = struct{}{}
	}
	plan.GeneratedFiles = plan.GeneratedFiles[:0]
	for path := range generated {
		plan.GeneratedFiles = append(plan.GeneratedFiles, path)
	}
	sort.Strings(plan.GeneratedFiles)
	return plan, nil
}
