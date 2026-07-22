package authenticationv1

import (
	"time"

	commonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/common/v1"
)

type AuthenticationAssertion struct {
	SchemaVersion     string          `json:"schemaVersion"`
	AssertionID       string          `json:"assertionId"`
	TransactionID     string          `json:"transactionId"`
	ProviderID        string          `json:"providerId"`
	ProviderProfileID string          `json:"providerProfileId"`
	Subject           SubjectIdentity `json:"subject"`
	TenantID          string          `json:"tenantId"`
	PortalID          string          `json:"portalId"`
	Audience          string          `json:"audience"`
	AMR               []string        `json:"amr"`
	ACR               string          `json:"acr"`
	IssuedAt          time.Time       `json:"issuedAt"`
	ExpiresAt         time.Time       `json:"expiresAt"`
	Nonce             string          `json:"nonce"`
}

type Signature = commonv1.Ed25519Signature

type SignedAuthenticationAssertion struct {
	Payload   AuthenticationAssertion `json:"payload"`
	Signature Signature               `json:"signature"`
}
