package profile

import (
	"context"
	"errors"
)

// DesktopIdentity is supplied by a verified Desktop enrollment transport, never
// decoded from a Profile request. Enrollment/certificate issuance remains a
// separate control-plane concern.
type DesktopIdentity struct {
	ID       string
	TenantID string
}
type Claim struct {
	ProfileID string
	Revision  uint64
	DesktopID string
	TenantID  string
	Plugins   []PluginRef
}

func ClaimLaunch(_ context.Context, identity DesktopIdentity, p Profile) (Claim, error) {
	if identity.ID == "" || identity.TenantID == "" {
		return Claim{}, errors.New("Desktop 领取必须携带经验证身份与 tenant")
	}
	if p.TenantID != identity.TenantID {
		return Claim{}, errors.New("Desktop 不得领取其他 tenant 的 Profile")
	}
	if !Eligible(p, identity.ID) {
		return Claim{}, errors.New("Desktop 未被分配此 Profile")
	}
	return Claim{ProfileID: p.ID, Revision: p.Revision, DesktopID: identity.ID, TenantID: identity.TenantID, Plugins: append([]PluginRef(nil), p.Plugins...)}, nil
}
