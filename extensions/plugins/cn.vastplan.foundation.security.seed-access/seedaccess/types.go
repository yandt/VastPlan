// Package seedaccess implements the database-independent setup and recovery
// state machine. It is a Foundation plugin domain package, not a kernel user
// system and not an enterprise identity directory.
package seedaccess

import (
	"time"

	authenticationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authentication/v1"
	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
)

const (
	PluginID      = "cn.vastplan.foundation.security.seed-access"
	PluginVersion = "0.1.0"
	StateVersion  = 1
)

type Phase string

const (
	Uninitialized      Phase = "uninitialized"
	SeedActive         Phase = "seed-active"
	ProviderConfigured Phase = "provider-configured"
	ProviderVerified   Phase = "provider-verified"
	HandoffReady       Phase = "handoff-ready"
	EnterpriseActive   Phase = "enterprise-active"
	RecoveryLease      Phase = "recovery-lease"
)

type Operator struct {
	ID       string           `json:"id"`
	Verifier PasswordVerifier `json:"verifier"`
}

type HandoffSeal struct {
	ProviderProfile compositioncommonv1.Ref          `json:"providerProfile"`
	Subject         authenticationv1.SubjectIdentity `json:"subject"`
	PolicySnapshot  compositioncommonv1.Ref          `json:"policySnapshot"`
	SessionID       string                           `json:"sessionId"`
	AuthenticatedAt time.Time                        `json:"authenticatedAt"`
	ExpiresAt       time.Time                        `json:"expiresAt"`
	RecoveryReady   bool                             `json:"recoveryReady"`
	Digest          string                           `json:"digest"`
}

type Lease struct {
	Digest    string    `json:"digest"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type State struct {
	Version         int                               `json:"version"`
	Generation      uint64                            `json:"generation"`
	Phase           Phase                             `json:"phase"`
	Operator        *Operator                         `json:"operator,omitempty"`
	ProviderProfile *compositioncommonv1.Ref          `json:"providerProfile,omitempty"`
	ProviderSubject *authenticationv1.SubjectIdentity `json:"providerSubject,omitempty"`
	Handoff         *HandoffSeal                      `json:"handoff,omitempty"`
	Recovery        *Lease                            `json:"recovery,omitempty"`
	UpdatedAt       time.Time                         `json:"updatedAt"`
}

type Store interface {
	Load() (State, error)
	Update(expectedGeneration uint64, next State) (State, error)
}

type LocalRecoveryVerifier interface {
	VerifyLocalRecoveryProof(proof []byte) error
}
