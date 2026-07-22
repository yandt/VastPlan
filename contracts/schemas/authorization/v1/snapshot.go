package authorizationv1

import "time"

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

type Signature struct {
	Algorithm string `json:"algorithm"`
	KeyID     string `json:"keyId"`
	Value     string `json:"value"`
}

type SignedPolicySnapshot struct {
	Payload   PolicySnapshot `json:"payload"`
	Signature Signature      `json:"signature"`
}
