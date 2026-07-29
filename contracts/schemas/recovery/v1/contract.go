package recoveryv1

import pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"

const (
	Version           = 1
	StageRecovery     = "recovery"
	StageControlPlane = "control-plane"
	StagePlatform     = "platform"
)

// Plan classifies every enabled Seed service into one cumulative readiness
// stage. It is deployment input, not runtime state.
type Plan struct {
	Version int     `json:"version"`
	ID      string  `json:"id"`
	Stages  []Stage `json:"stages"`
}

type Stage struct {
	ID    string            `json:"id"`
	Units []UnitRequirement `json:"units"`
}

type UnitRequirement struct {
	ID       string `json:"id"`
	MinReady uint16 `json:"minReady"`
}

// Capsule binds a reviewed Plan to the exact Bootstrap LKG artifacts that can
// restore its recovery stage without consulting the managed repository.
type Capsule struct {
	Version   int              `json:"version"`
	ID        string           `json:"id"`
	Inventory InventoryBinding `json:"inventory"`
	Artifacts []Artifact       `json:"artifacts"`
	Stages    []Stage          `json:"stages"`
}

type InventoryBinding struct {
	RepositoryID string `json:"repositoryId"`
	Generation   uint64 `json:"generation"`
}

type Artifact struct {
	Ref    pluginv1.ArtifactRef `json:"ref"`
	SHA256 string               `json:"sha256"`
}
