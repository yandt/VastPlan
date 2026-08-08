// Package platformcontrolv1 defines the non-secret bootstrap profile and the
// bounded state projected to the Seed recovery/configuration UI.
package platformcontrolv1

import databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"

const (
	SchemaURL           = "https://schemas.cdsoft.com.cn/vastplan/platformcontrol/v1/vastplan.platform-control.schema.json"
	Version             = 1
	BootstrapCapability = "foundation.state.shared.sql.bootstrap"
	// 3.1.0 adds the additive close operation. A 3.0.0 replica stays valid; it
	// simply cannot be told to release an abandoned candidate pool.
	BootstrapContractVersion = "3.1.0"
	RuntimeLogicalService    = "foundation.data.relational.runtime"
	RuntimeRoutingDomain     = "platform"
	OperationTest            = "test"
	OperationProvision       = "provision"
	OperationInitialize      = "initialize"
	OperationOpen            = "open"
	// OperationClose notifies a Runtime replica that the host is releasing a
	// candidate pool it opened but will not commit. The payload carries the
	// generation to avoid closing a newer pool that replaced the candidate.
	OperationClose            = "close"
	TrustedBootstrapSystemID  = "platform-control-bootstrap/primary"
	TrustedBootstrapScene     = "platform.control.bootstrap"
	ErrorInvalid              = "platform.control.invalid"
	ErrorUnavailable          = "platform.control.unavailable"
	ErrorConflict             = "platform.control.conflict"
	ErrorProvisioningFailed   = "platform.control.provisioning_failed"
	ErrorInitializationFailed = "platform.control.initialization_failed"
	KernelStatusService       = "kernel.platform-control.status"
	KernelTestService         = "kernel.platform-control.test"
	KernelConfigureService    = "kernel.platform-control.configure"
	MaxSecretMaterialBytes    = 64 << 10
)

// CloseRequest releases a candidate pool on one Runtime replica. It carries no
// secret and no connection detail: the replica already holds everything it
// needs, and the generation only has to be precise enough to avoid closing a
// pool that a later activation already replaced.
type CloseRequest struct {
	Generation uint64 `json:"generation"`
}

type SecretRef struct {
	Kind string `json:"kind"`
	Name string `json:"name,omitempty"`
	Path string `json:"path,omitempty"`
}

type Profile struct {
	SchemaVersion int                            `json:"schemaVersion"`
	Generation    uint64                         `json:"generation"`
	Connection    databasev1.ConnectionCandidate `json:"connection"`
	Schema        string                         `json:"schema"`
	SecretRef     SecretRef                      `json:"secretRef"`
	ContractRange string                         `json:"contractRange"`
}

type Phase string

const (
	PhaseUnconfigured Phase = "unconfigured"
	PhaseTesting      Phase = "testing"
	PhaseProvisioning Phase = "provisioning"
	PhaseInitializing Phase = "initializing"
	PhaseReady        Phase = "ready"
	PhaseRecovery     Phase = "recovery"
)

type Status struct {
	Phase      Phase    `json:"phase"`
	Generation uint64   `json:"generation,omitempty"`
	Code       string   `json:"code,omitempty"`
	Profile    *Profile `json:"profile,omitempty"`
}

type ChangeRequest struct {
	Profile                 Profile `json:"profile"`
	ExpectedGeneration      uint64  `json:"expectedGeneration"`
	SecretMaterial          string  `json:"secretMaterial,omitempty"`
	CreateDatabaseIfMissing bool    `json:"createDatabaseIfMissing,omitempty"`
}
