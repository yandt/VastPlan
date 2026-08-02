// Package approvalv2 defines the framework-neutral Approval Policy Provider
// contract. Business plugins submit trusted facts; replaceable providers own
// policy interpretation and return bounded decisions.
package approvalv2

import "encoding/json"

const (
	Protocol   = "approval.policy.v2"
	Capability = "foundation.security.approval-policy"
)

type ProfileRef struct {
	ID       string `json:"id"`
	Revision uint64 `json:"revision"`
	Digest   string `json:"digest"`
}

type ProviderBinding struct {
	Protocol       string     `json:"protocol"`
	Capability     string     `json:"capability"`
	LogicalService string     `json:"logicalService"`
	RoutingDomain  string     `json:"routingDomain"`
	Profile        ProfileRef `json:"profile"`
}

type Effect string

const (
	EffectAllow           Effect = "allow"
	EffectDeny            Effect = "deny"
	EffectRequireEvidence Effect = "require-evidence"
)

type Operator string

const (
	OperatorEquals      Operator = "equals"
	OperatorNotEquals   Operator = "not-equals"
	OperatorContains    Operator = "contains"
	OperatorNotContains Operator = "not-contains"
	OperatorExists      Operator = "exists"
	OperatorNotExists   Operator = "not-exists"
)

type Condition struct {
	Left      string   `json:"left"`
	Operator  Operator `json:"operator"`
	RightFact string   `json:"rightFact,omitempty"`
	Value     string   `json:"value,omitempty"`
}

type EvidenceKind string

const (
	EvidenceExactFactMatch EvidenceKind = "exact-fact-match"
	EvidenceBooleanTrue    EvidenceKind = "boolean-true"
	EvidenceTextLength     EvidenceKind = "text-length"
)

type EvidenceRequirement struct {
	ID        string       `json:"id"`
	Field     string       `json:"field"`
	Kind      EvidenceKind `json:"kind"`
	Fact      string       `json:"fact,omitempty"`
	MinLength int          `json:"minLength,omitempty"`
	MaxLength int          `json:"maxLength,omitempty"`
	Title     string       `json:"title,omitempty"`
	Audit     bool         `json:"audit,omitempty"`
}

type Rule struct {
	ID           string                `json:"id"`
	Priority     int                   `json:"priority"`
	Conditions   []Condition           `json:"conditions"`
	Effect       Effect                `json:"effect"`
	Requirements []EvidenceRequirement `json:"requirements,omitempty"`
	Code         string                `json:"code,omitempty"`
	Message      string                `json:"message,omitempty"`
}

type PolicyProfile struct {
	ID            string `json:"id"`
	Revision      uint64 `json:"revision"`
	DefaultEffect Effect `json:"defaultEffect"`
	Rules         []Rule `json:"rules"`
}

type ActorFacts struct {
	ID    string   `json:"id"`
	Roles []string `json:"roles,omitempty"`
}

type ResourceFacts struct {
	ID          string            `json:"id"`
	Digest      string            `json:"digest"`
	SubmittedBy string            `json:"submittedBy,omitempty"`
	Attributes  map[string]string `json:"attributes,omitempty"`
}

type EvaluationInput struct {
	Operation string                     `json:"operation"`
	TenantID  string                     `json:"tenantId"`
	Actor     ActorFacts                 `json:"actor"`
	Resource  ResourceFacts              `json:"resource"`
	Context   map[string]string          `json:"context,omitempty"`
	Evidence  map[string]json.RawMessage `json:"evidence,omitempty"`
}

// ReviewEvidence is the Portal control API's bounded projection. Business
// plugins translate it into namespaced evidence fields before Provider calls.
type ReviewEvidence struct {
	ExpectedDigest string `json:"expectedDigest,omitempty"`
	Acknowledged   bool   `json:"acknowledged,omitempty"`
	Reason         string `json:"reason,omitempty"`
}

type EvaluateRequest struct {
	Profile ProfileRef      `json:"profile"`
	Input   EvaluationInput `json:"input"`
}

type EvaluateBatchRequest struct {
	Profile ProfileRef        `json:"profile"`
	Inputs  []EvaluationInput `json:"inputs"`
}

type DecisionStatus string

const (
	DecisionAllowed        DecisionStatus = "allowed"
	DecisionReviewRequired DecisionStatus = "review-required"
	DecisionDenied         DecisionStatus = "denied"
)

type Decision struct {
	Status        DecisionStatus        `json:"status"`
	Profile       ProfileRef            `json:"profile"`
	MatchedRuleID string                `json:"matchedRuleId,omitempty"`
	Code          string                `json:"code,omitempty"`
	Message       string                `json:"message,omitempty"`
	AuditNote     string                `json:"auditNote,omitempty"`
	Requirements  []EvidenceRequirement `json:"requirements,omitempty"`
}

type EvaluateResult struct {
	Decision Decision `json:"decision"`
}

type EvaluateBatchResult struct {
	Decisions []Decision `json:"decisions"`
}

type HealthResult struct {
	Ready    bool         `json:"ready"`
	Profiles []ProfileRef `json:"profiles"`
}
