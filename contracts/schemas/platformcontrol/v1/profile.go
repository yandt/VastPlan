// Package platformcontrolv1 defines the non-secret bootstrap profile and the
// bounded state projected to the Seed recovery/configuration UI.
package platformcontrolv1

import databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"

const (
	SchemaURL                 = "https://schemas.cdsoft.com.cn/vastplan/platformcontrol/v1/vastplan.platform-control.schema.json"
	Version                   = 1
	BootstrapCapability       = "foundation.state.shared.sql.bootstrap"
	BootstrapContractVersion  = "3.0.0"
	RuntimeLogicalService     = "foundation.data.relational.runtime"
	RuntimeRoutingDomain      = "platform"
	OperationTest             = "test"
	OperationProvision        = "provision"
	OperationInitialize       = "initialize"
	OperationOpen             = "open"
	TrustedBootstrapSystemID  = "platform-control-bootstrap/primary"
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
