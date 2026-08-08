// Package model defines the neutral data exchanged between Node Agent
// reconciliation and Runtime Host execution.
package model

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

// InstalledPlugin is a verified installation that can be started by a backend host.
type InstalledPlugin struct {
	ID                   string                    `json:"id"`
	Publisher            string                    `json:"publisher"`
	Version              string                    `json:"version"`
	InterfaceFingerprint string                    `json:"interface_fingerprint"`
	Engines              map[string]string         `json:"engines,omitempty"`
	Channel              string                    `json:"channel"`
	SHA256               string                    `json:"sha256"`
	Root                 string                    `json:"root"`
	EntryPath            string                    `json:"entry_path"`
	DynamicGoPath        string                    `json:"dynamic_go_path,omitempty"`
	PythonPath           string                    `json:"python_path,omitempty"`
	Execution            pluginv1.BackendExecution `json:"execution"`
	State                *PluginStateContract      `json:"state,omitempty"`
	Contract             PluginRuntimeContract     `json:"contract"`
}

// PluginRuntimeContract is the runtime authorization frozen from a signed manifest.
type PluginRuntimeContract struct {
	Contributions     []pluginv1.RuntimeContribution `json:"contributions"`
	Requires          []pluginv1.RuntimeRequirement  `json:"requires,omitempty"`
	KernelServices    []string                       `json:"kernel_services,omitempty"`
	ContextAccess     pluginv1.ContextAccess         `json:"context_access,omitempty"`
	BackgroundService bool                           `json:"background_service,omitempty"`
}

// PluginStateIdentity identifies a plugin-private state format without exposing storage.
type PluginStateIdentity struct {
	Format        string `json:"format"`
	FormatVersion int32  `json:"format_version"`
}

// PluginStateIdentityFromContract converts the signed contract identity.
func PluginStateIdentityFromContract(identity pluginv1.StateIdentity) PluginStateIdentity {
	return PluginStateIdentity{Format: identity.Format, FormatVersion: identity.FormatVersion}
}

// ContractIdentity converts the neutral identity back to its wire contract.
func (i PluginStateIdentity) ContractIdentity() pluginv1.StateIdentity {
	return pluginv1.StateIdentity{Format: i.Format, FormatVersion: i.FormatVersion}
}

// PluginStateContract is persisted with an installed artifact for future upgrades.
type PluginStateContract struct {
	PluginStateIdentity
	MigrationProtocol string                `json:"migration_protocol,omitempty"`
	MigrationFrom     []PluginStateIdentity `json:"migration_from,omitempty"`
}

// RuntimeUnit is the complete immutable composition consumed by Runtime Host.
type RuntimeUnit struct {
	ID                     string
	Fingerprint            string
	ServiceRole            string
	LogicalService         string
	InstancePolicy         string
	StateModel             string
	Visibility             string
	Routing                string
	RoutingDomain          string
	StartupTier            string
	Generation             uint64
	FencingToken           string
	PartitionKeys          []string
	PartitionGenerations   map[string]uint64
	PartitionFencingTokens map[string]string
	EnvironmentAllowlists  map[string][]string
	KernelServiceGrants    map[string][]string
	Config                 map[string]any
	Plugins                []InstalledPlugin
	Migrations             []StateMigrationPlan
	RestartBase            uint64
	ClusterMaxReplicas     int
}

// ReplacementCandidate is host-only evidence emitted before an old generation retires.
type ReplacementCandidate struct {
	UnitID             string
	StartupTier        string
	Replacing          bool
	RuntimeInstanceIDs []string
}

// ReplacementReadinessBarrier waits for external adoption of candidate instances.
type ReplacementReadinessBarrier interface {
	AwaitReady(context.Context, ReplacementCandidate) error
}

// StateMigrationPlan describes one idempotent lifecycle.v1 migration.
type StateMigrationPlan struct {
	PluginID      string
	TransactionID string
	From          PluginStateIdentity
	To            PluginStateIdentity
}

// StateMigrationError distinguishes migration failures from process launch failures.
type StateMigrationError struct {
	PluginID string
	Phase    string
	Err      error
}

func (e *StateMigrationError) Error() string {
	return fmt.Sprintf("插件 %s 状态迁移 %s 失败: %v", e.PluginID, e.Phase, e.Err)
}

func (e *StateMigrationError) Unwrap() error { return e.Err }

// RuntimeStatus reports facts observed from live plugin sessions.
type RuntimeStatus struct {
	Healthy             bool
	Readiness           string
	DependencyIssues    []string
	PIDs                []int
	StartedAt           time.Time
	RestartCount        uint64
	KernelServiceGrants map[string][]string
}

// RuntimeEvent notifies the Agent that a unit's runtime facts changed.
type RuntimeEvent struct {
	UnitID      string
	Fingerprint string
	Type        string
	Message     string
	OccurredAt  time.Time
}

// RawConfig clones JSON configuration before Runtime Host retains it.
func RawConfig(config map[string]any) map[string]any {
	if config == nil {
		return nil
	}
	raw, _ := json.Marshal(config)
	var clone map[string]any
	_ = json.Unmarshal(raw, &clone)
	return clone
}

// CloneUint64Map copies a map retained across reconciliation boundaries.
func CloneUint64Map(input map[string]uint64) map[string]uint64 {
	if input == nil {
		return nil
	}
	out := make(map[string]uint64, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

// CloneStringSlices copies a string-slice map retained across reconciliation boundaries.
func CloneStringSlices(input map[string][]string) map[string][]string {
	if input == nil {
		return nil
	}
	out := make(map[string][]string, len(input))
	for key, values := range input {
		out[key] = append([]string(nil), values...)
	}
	return out
}

// CloneStringMap copies a string map retained across reconciliation boundaries.
func CloneStringMap(input map[string]string) map[string]string {
	if input == nil {
		return nil
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}
