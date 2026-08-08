package runtimehost

import "cdsoft.com.cn/VastPlan/core/kernels/backend/nodeagent/model"

type InstalledPlugin = model.InstalledPlugin
type PluginRuntimeContract = model.PluginRuntimeContract
type PluginStateIdentity = model.PluginStateIdentity
type PluginStateContract = model.PluginStateContract
type RuntimeUnit = model.RuntimeUnit
type ReplacementCandidate = model.ReplacementCandidate
type ReplacementReadinessBarrier = model.ReplacementReadinessBarrier
type StateMigrationPlan = model.StateMigrationPlan
type StateMigrationError = model.StateMigrationError
type RuntimeStatus = model.RuntimeStatus
type RuntimeEvent = model.RuntimeEvent

func RawConfig(config map[string]any) map[string]any {
	return model.RawConfig(config)
}
