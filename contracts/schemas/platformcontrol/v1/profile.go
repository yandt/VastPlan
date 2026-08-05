// Package platformcontrolv1 defines the non-secret bootstrap profile and the
// bounded state projected to the Seed recovery/configuration UI.
package platformcontrolv1

const (
	SchemaURL                 = "https://schemas.cdsoft.com.cn/vastplan/platformcontrol/v1/vastplan.platform-control.schema.json"
	Version                   = 1
	BootstrapCapability       = "foundation.state.shared.sql.bootstrap"
	BootstrapContractVersion  = "1.0.0"
	RuntimeLogicalService     = "foundation.data.relational.runtime"
	RuntimeRoutingDomain      = "platform"
	OperationTest             = "test"
	OperationInitialize       = "initialize"
	OperationOpen             = "open"
	TrustedBootstrapSystemID  = "platform-control-bootstrap/primary"
	ErrorInvalid              = "platform.control.invalid"
	ErrorUnavailable          = "platform.control.unavailable"
	ErrorConflict             = "platform.control.conflict"
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

type TLS struct {
	Mode       string `json:"mode"`
	ServerName string `json:"serverName,omitempty"`
}

type Profile struct {
	SchemaVersion int       `json:"schemaVersion"`
	Generation    uint64    `json:"generation"`
	ProviderID    string    `json:"providerId"`
	Endpoint      string    `json:"endpoint"`
	Database      string    `json:"database"`
	Schema        string    `json:"schema"`
	TLS           TLS       `json:"tls"`
	Username      string    `json:"username"`
	SecretRef     SecretRef `json:"secretRef"`
	ContractRange string    `json:"contractRange"`
}

type Phase string

const (
	PhaseUnconfigured Phase = "unconfigured"
	PhaseTesting      Phase = "testing"
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
	Profile            Profile `json:"profile"`
	ExpectedGeneration uint64  `json:"expectedGeneration"`
	SecretMaterial     string  `json:"secretMaterial,omitempty"`
}
