// Package pluginlibrarysource projects development source directories into
// immutable workspace artifacts in the Local Plugin Library. It is an input
// adapter, never a runtime discovery source or activation authority.
package pluginlibrarysource

import (
	"time"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

const stateSchemaVersion = 1

type Phase string

const (
	PhaseObserved    Phase = "Observed"
	PhaseDebouncing  Phase = "Debouncing"
	PhaseBuilding    Phase = "Building"
	PhasePublishing  Phase = "Publishing"
	PhaseReady       Phase = "Ready"
	PhaseWithdrawing Phase = "Withdrawing"
	PhaseRemoved     Phase = "Removed"
	PhaseFailed      Phase = "Failed"
)

type SourceState struct {
	SourceID          string                `json:"sourceId"`
	PluginID          string                `json:"pluginId,omitempty"`
	Fingerprint       string                `json:"fingerprint,omitempty"`
	SourceDigest      string                `json:"sourceDigest,omitempty"`
	ActiveRef         *pluginv1.ArtifactRef `json:"activeRef,omitempty"`
	PendingWithdrawal *pluginv1.ArtifactRef `json:"pendingWithdrawal,omitempty"`
	Phase             Phase                 `json:"phase"`
	LastError         string                `json:"lastError,omitempty"`
	UpdatedAt         time.Time             `json:"updatedAt"`
}

type State struct {
	SchemaVersion int                    `json:"schemaVersion"`
	Initialized   bool                   `json:"initialized"`
	Generation    uint64                 `json:"generation"`
	UpdatedAt     time.Time              `json:"updatedAt"`
	Sources       map[string]SourceState `json:"sources"`
}
