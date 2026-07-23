package repositoryruntime

import (
	"errors"
	"time"
)

const (
	defaultPublicationApprovalTTLHours = 7 * 24
	maxPublicationApprovalTTLHours     = 30 * 24
)

// PublicationPolicy controls the trusted server-side stable publication window.
// A zero value selects the conservative seven-day default.
type PublicationPolicy struct {
	ApprovalTTLHours int `json:"approvalTTLHours"`
}

func (p PublicationPolicy) normalized() PublicationPolicy {
	if p.ApprovalTTLHours == 0 {
		p.ApprovalTTLHours = defaultPublicationApprovalTTLHours
	}
	return p
}

func (p PublicationPolicy) Validate() error {
	p = p.normalized()
	if p.ApprovalTTLHours < 1 || p.ApprovalTTLHours > maxPublicationApprovalTTLHours {
		return errors.New("发布审批有效期必须在 1 到 720 小时之间")
	}
	return nil
}

func (p PublicationPolicy) approvalTTL() time.Duration {
	return time.Duration(p.normalized().ApprovalTTLHours) * time.Hour
}
