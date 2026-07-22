package authorizationv1

import "time"

type EnginePrepareRequest struct {
	Snapshot SignedPolicySnapshot `json:"snapshot"`
}

type EnginePrepareResult struct {
	Handle       string    `json:"handle"`
	PolicyDigest string    `json:"policyDigest"`
	ExpiresAt    time.Time `json:"expiresAt"`
}

type EvaluationTarget struct {
	ExtensionPoint string `json:"extensionPoint"`
	Capability     string `json:"capability"`
	Operation      string `json:"operation"`
}

type EvaluationScope struct {
	TenantID     string `json:"tenantId,omitempty"`
	ProjectID    string `json:"projectId,omitempty"`
	ResourceType string `json:"resourceType,omitempty"`
	ResourceID   string `json:"resourceId,omitempty"`
}

type EvaluationInput struct {
	RequestID           string           `json:"requestId"`
	PolicyDigest        string           `json:"policyDigest"`
	DomainID            string           `json:"domainId"`
	Subject             Subject          `json:"subject"`
	ExternalGroups      []ExternalGroup  `json:"externalGroups"`
	Target              EvaluationTarget `json:"target"`
	Scope               EvaluationScope  `json:"scope"`
	RequiredPermissions []string         `json:"requiredPermissions"`
	ContextDigest       string           `json:"contextDigest"`
	EvaluatedAt         time.Time        `json:"evaluatedAt"`
}

type EngineEvaluateRequest struct {
	Handle string          `json:"handle"`
	Input  EvaluationInput `json:"input"`
}

type AuthorizationDecision string

const (
	DecisionAllow         AuthorizationDecision = "allow"
	DecisionDeny          AuthorizationDecision = "deny"
	DecisionIndeterminate AuthorizationDecision = "indeterminate"
)

type DecisionProof struct {
	ProofID            string                `json:"proofId"`
	ProviderID         string                `json:"providerId"`
	PolicyDigest       string                `json:"policyDigest"`
	InputDigest        string                `json:"inputDigest"`
	Decision           AuthorizationDecision `json:"decision"`
	ReasonCode         string                `json:"reasonCode"`
	MatchedRoleIDs     []string              `json:"matchedRoleIds"`
	MatchedBindingIDs  []string              `json:"matchedBindingIds"`
	RevocationRevision uint64                `json:"revocationRevision"`
	EvaluatedAt        time.Time             `json:"evaluatedAt"`
	ValidUntil         time.Time             `json:"validUntil"`
}

type EngineEvaluateResult struct {
	Decision AuthorizationDecision `json:"decision"`
	Proof    DecisionProof         `json:"proof"`
}

type EngineExplainRequest struct {
	Handle  string `json:"handle"`
	ProofID string `json:"proofId"`
}

type ExplanationStep struct {
	Code       string   `json:"code"`
	Outcome    string   `json:"outcome"`
	References []string `json:"references"`
}

type EngineExplainResult struct {
	ProofID string            `json:"proofId"`
	Steps   []ExplanationStep `json:"steps"`
}

type EngineHealthRequest struct{}

type EngineHealthResult struct {
	Ready            bool   `json:"ready"`
	PreparedPolicies int    `json:"preparedPolicies"`
	ProviderID       string `json:"providerId"`
}
