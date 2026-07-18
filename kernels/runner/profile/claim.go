package profile

import (
	"context"
	"errors"
)

// RunnerIdentity is supplied by a verified Runner enrollment transport, never
// decoded from a Profile request. Enrollment/certificate issuance remains a
// separate control-plane concern.
type RunnerIdentity struct {
	ID       string
	TenantID string
}
type Claim struct {
	ProfileID string
	Revision  uint64
	RunnerID  string
	TenantID  string
	Plugins   []PluginRef
}

func ClaimLaunch(_ context.Context, identity RunnerIdentity, p Profile) (Claim, error) {
	if identity.ID == "" || identity.TenantID == "" {
		return Claim{}, errors.New("Runner 领取必须携带经验证身份与 tenant")
	}
	if p.TenantID != identity.TenantID {
		return Claim{}, errors.New("Runner 不得领取其他 tenant 的 Profile")
	}
	if !Eligible(p, identity.ID) {
		return Claim{}, errors.New("Runner 未被分配此 Profile")
	}
	return Claim{ProfileID: p.ID, Revision: p.Revision, RunnerID: identity.ID, TenantID: identity.TenantID, Plugins: append([]PluginRef(nil), p.Plugins...)}, nil
}
