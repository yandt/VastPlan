package pluginreconcile

import (
	"errors"
	"sort"

	appv1 "cdsoft.com.cn/VastPlan/contracts/schemas/app/v1"
	deploymentv1 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

type FrontendActivationPlan struct {
	Generation uint64                 `json:"generation"`
	Plugins    []pluginv1.ArtifactRef `json:"plugins"`
	HostEpoch  bool                   `json:"hostEpoch"`
}

type MobileBundlePlan struct {
	Generation uint64                            `json:"generation"`
	Plugins    []pluginv1.PluginArtifactIdentity `json:"plugins"`
	Republish  bool                              `json:"republish"`
}

func ApplyBackendUnit(plan pluginv1.ReconciliationPlan, unit deploymentv1.Unit) (deploymentv1.Unit, error) {
	if plan.Target != pluginv1.PluginTargetBackend {
		return deploymentv1.Unit{}, errors.New("Backend Unit 不能消费其他内核计划")
	}
	artifacts, err := desiredArtifacts(plan)
	if err != nil {
		return deploymentv1.Unit{}, err
	}
	unit.Plugins = make([]deploymentv1.PluginRef, 0, len(artifacts))
	for _, artifact := range artifacts {
		unit.Plugins = append(unit.Plugins, deploymentv1.PluginRef{ID: artifact.Ref.PluginID, Version: artifact.Ref.Version, Channel: artifact.Ref.Channel, SHA256: artifact.SHA256})
	}
	return unit, nil
}

func BuildFrontendActivation(plan pluginv1.ReconciliationPlan) (FrontendActivationPlan, error) {
	if plan.Target != pluginv1.PluginTargetFrontend {
		return FrontendActivationPlan{}, errors.New("Frontend Activation 不能消费其他内核计划")
	}
	artifacts, err := desiredArtifacts(plan)
	if err != nil {
		return FrontendActivationPlan{}, err
	}
	result := FrontendActivationPlan{Generation: plan.Generation, Plugins: make([]pluginv1.ArtifactRef, 0, len(artifacts))}
	for _, artifact := range artifacts {
		result.Plugins = append(result.Plugins, artifact.Ref)
	}
	for _, action := range plan.Actions {
		if action.Strategy == "frontend.host-epoch" {
			result.HostEpoch = true
		}
	}
	return result, nil
}

func ApplyRunnerProfile(plan pluginv1.ReconciliationPlan, profile appv1.Profile) (appv1.Profile, error) {
	if plan.Target != pluginv1.PluginTargetRunner {
		return appv1.Profile{}, errors.New("Runner Profile 不能消费其他内核计划")
	}
	artifacts, err := desiredArtifacts(plan)
	if err != nil {
		return appv1.Profile{}, err
	}
	profile.Revision = plan.Generation
	profile.Plugins = make([]appv1.PluginRef, 0, len(artifacts))
	for _, artifact := range artifacts {
		profile.Plugins = append(profile.Plugins, appv1.PluginRef{ID: artifact.Ref.PluginID, Version: artifact.Ref.Version, Channel: artifact.Ref.Channel})
	}
	return appv1.Validate(profile)
}

func BuildMobileBundle(plan pluginv1.ReconciliationPlan) (MobileBundlePlan, error) {
	if plan.Target != pluginv1.PluginTargetMobile {
		return MobileBundlePlan{}, errors.New("Mobile Bundle 不能消费其他内核计划")
	}
	artifacts, err := desiredArtifacts(plan)
	if err != nil {
		return MobileBundlePlan{}, err
	}
	result := MobileBundlePlan{Generation: plan.Generation, Plugins: artifacts}
	for _, action := range plan.Actions {
		if action.Operation != pluginv1.ReconcileRetain {
			result.Republish = true
		}
	}
	return result, nil
}

func desiredArtifacts(plan pluginv1.ReconciliationPlan) ([]pluginv1.PluginArtifactIdentity, error) {
	if err := pluginv1.ValidateReconciliationPlan(plan); err != nil {
		return nil, err
	}
	values := []pluginv1.PluginArtifactIdentity{}
	for _, action := range plan.Actions {
		if action.Operation == pluginv1.ReconcileDeactivate {
			continue
		}
		selected := action.Candidate
		if selected == nil {
			selected = action.Current
		}
		if selected == nil {
			return nil, errors.New("Reconciliation Action 缺少最终制品")
		}
		values = append(values, *selected)
	}
	sort.Slice(values, func(i, j int) bool { return values[i].Ref.PluginID < values[j].Ref.PluginID })
	return values, nil
}
