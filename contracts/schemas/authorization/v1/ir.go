// Package authorizationv1 defines the language-neutral VastPlan
// authorization IR and provider wire protocols. It contains no policy engine,
// identity SDK, storage client, or plugin runtime objects.
package authorizationv1

import "time"

const (
	IRSchemaVersion = "v1"
	IRSchemaURL     = "https://schemas.cdsoft.com.cn/vastplan/authorization/v1/vastplan.authorization-ir.schema.json"
)

type DomainKind string

const (
	DomainPlatform DomainKind = "platform"
	DomainTenant   DomainKind = "tenant"
	DomainProject  DomainKind = "project"
	DomainResource DomainKind = "resource"
)

type Risk string

const (
	RiskLow      Risk = "low"
	RiskMedium   Risk = "medium"
	RiskHigh     Risk = "high"
	RiskCritical Risk = "critical"
)

type SubjectKind string

const (
	SubjectUser    SubjectKind = "user"
	SubjectGroup   SubjectKind = "group"
	SubjectService SubjectKind = "service"
	SubjectDevice  SubjectKind = "device"
)

type StatementEffect string

const (
	EffectAllow StatementEffect = "allow"
	EffectDeny  StatementEffect = "deny"
)

type PolicyDomain struct {
	ID                string            `json:"id"`
	Revision          uint64            `json:"revision"`
	Kind              DomainKind        `json:"kind"`
	ParentID          string            `json:"parentId,omitempty"`
	Scope             DomainScope       `json:"scope"`
	ProviderProfileID string            `json:"providerProfileId"`
	Delegation        DelegationCeiling `json:"delegation"`
}

// DomainScope is explicit so a child cannot manufacture a tenant, project, or
// resource identity outside its verified parent chain.
type DomainScope struct {
	TenantID     string `json:"tenantId,omitempty"`
	ProjectID    string `json:"projectId,omitempty"`
	ResourceType string `json:"resourceType,omitempty"`
	ResourceID   string `json:"resourceId,omitempty"`
}

type DelegationCeiling struct {
	Permissions    []string `json:"permissions"`
	MaxRisk        Risk     `json:"maxRisk"`
	MayDelegate    bool     `json:"mayDelegate"`
	OfflineAllowed bool     `json:"offlineAllowed"`
	MaxTTLSeconds  int64    `json:"maxTtlSeconds"`
}

// AuthorizationIR is the canonical provider-independent representation of a
// published policy. OR is represented by multiple statements; constraints in
// one statement are ANDed. This bounded form prevents provider-specific policy
// languages from leaking into the platform contract.
type AuthorizationIR struct {
	SchemaVersion      string            `json:"schemaVersion"`
	CatalogDigest      string            `json:"catalogDigest"`
	RootDomainID       string            `json:"rootDomainId"`
	ProviderProfiles   []ProviderProfile `json:"providerProfiles"`
	Domains            []PolicyDomain    `json:"domains"`
	Roles              []CompiledRole    `json:"roles"`
	Bindings           []SubjectBinding  `json:"bindings"`
	Revocations        []Revocation      `json:"revocations"`
	RevocationRevision uint64            `json:"revocationRevision"`
}

type CompiledRole struct {
	ID         string            `json:"id"`
	Revision   uint64            `json:"revision"`
	DomainID   string            `json:"domainId"`
	Statements []PolicyStatement `json:"statements"`
}

type PolicyStatement struct {
	ID          string                `json:"id"`
	Effect      StatementEffect       `json:"effect"`
	Permissions []string              `json:"permissions"`
	Resource    *ResourceSelector     `json:"resource,omitempty"`
	Constraints []AttributeConstraint `json:"constraints"`
}

type ResourceSelector struct {
	Type      string              `json:"type"`
	IDs       []string            `json:"ids"`
	Labels    map[string][]string `json:"labels"`
	Ownership string              `json:"ownership"`
}

// AttributeConstraint is deliberately finite and string based. Providers may
// compile it to another policy language, but cannot add hidden semantics.
type AttributeConstraint struct {
	Source   string   `json:"source"`
	Key      string   `json:"key"`
	Operator string   `json:"operator"`
	Values   []string `json:"values"`
}

type Subject struct {
	Kind   SubjectKind `json:"kind"`
	ID     string      `json:"id"`
	Issuer string      `json:"issuer,omitempty"`
}

type SubjectBinding struct {
	ID           string    `json:"id"`
	Revision     uint64    `json:"revision"`
	DomainID     string    `json:"domainId"`
	Subject      Subject   `json:"subject"`
	RoleID       string    `json:"roleId"`
	RoleRevision uint64    `json:"roleRevision"`
	NotBefore    time.Time `json:"notBefore"`
	ExpiresAt    time.Time `json:"expiresAt"`
}

type Revocation struct {
	ID          string    `json:"id"`
	Revision    uint64    `json:"revision"`
	Kind        string    `json:"kind"`
	TargetID    string    `json:"targetId"`
	EffectiveAt time.Time `json:"effectiveAt"`
	ReasonCode  string    `json:"reasonCode"`
}
