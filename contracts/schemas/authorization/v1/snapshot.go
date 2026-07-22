package authorizationv1

import (
	"time"

	commonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/common/v1"
)

type PolicySnapshot struct {
	SchemaVersion string          `json:"schemaVersion"`
	SnapshotID    string          `json:"snapshotId"`
	Revision      uint64          `json:"revision"`
	Audience      []string        `json:"audience"`
	IssuedAt      time.Time       `json:"issuedAt"`
	NotBefore     time.Time       `json:"notBefore"`
	ExpiresAt     time.Time       `json:"expiresAt"`
	Policy        AuthorizationIR `json:"policy"`
}

type Signature = commonv1.Ed25519Signature

type SignedPolicySnapshot struct {
	Payload   PolicySnapshot `json:"payload"`
	Signature Signature      `json:"signature"`
}
