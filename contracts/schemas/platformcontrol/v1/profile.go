// Package platformcontrolv1 defines the non-secret bootstrap profile and the
// bounded state projected to the Seed recovery/configuration UI.
package platformcontrolv1

const (
	SchemaURL = "https://schemas.cdsoft.com.cn/vastplan/platformcontrol/v1/vastplan.platform-control.schema.json"
	Version   = 1
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
	Phase      Phase  `json:"phase"`
	Generation uint64 `json:"generation,omitempty"`
	Code       string `json:"code,omitempty"`
}
