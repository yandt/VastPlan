// Package authorizationpolicy owns the versioned authorization policy state,
// approval workflow and signed Policy Snapshot publication.
package authorizationpolicy

import (
	"time"

	authorizationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authorization/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

const (
	PluginID      = "cn.vastplan.platform.security.authorization-policy"
	PluginVersion = "0.8.1"
	Capability    = "platform.authorization"
	stateVersion  = 1
)

type LifecycleState string

type PermissionSelectorKind string

const (
	StateDraft           LifecycleState = "Draft"
	StatePendingApproval LifecycleState = "PendingApproval"
	StateApproved        LifecycleState = "Approved"
	StatePublished       LifecycleState = "Published"
	StateRetired         LifecycleState = "Retired"
)

const (
	PermissionSelectorExact PermissionSelectorKind = "exact"
	PermissionSelectorGlob  PermissionSelectorKind = "glob"
)

type PermissionSelector struct {
	Kind  PermissionSelectorKind `json:"kind"`
	Value string                 `json:"value"`
}

type RoleStatementInput struct {
	ID                  string                                `json:"id"`
	Effect              authorizationv1.StatementEffect       `json:"effect"`
	PermissionSelectors []PermissionSelector                  `json:"permissionSelectors"`
	Resource            *authorizationv1.ResourceSelector     `json:"resource,omitempty"`
	Constraints         []authorizationv1.AttributeConstraint `json:"constraints"`
}

type StatementPermissionSelectors struct {
	StatementID string               `json:"statementId"`
	Selectors   []PermissionSelector `json:"selectors"`
}

type RoleRevision struct {
	ID                    string                            `json:"id"`
	Revision              uint64                            `json:"revision"`
	DomainID              string                            `json:"domainId"`
	Title                 string                            `json:"title"`
	Description           string                            `json:"description,omitempty"`
	SelectorCatalogDigest string                            `json:"selectorCatalogDigest"`
	PermissionSelectors   []StatementPermissionSelectors    `json:"permissionSelectors"`
	Statements            []authorizationv1.PolicyStatement `json:"statements"`
	State                 LifecycleState                    `json:"state"`
	CreatedBy             string                            `json:"createdBy"`
	ApprovedBy            string                            `json:"approvedBy,omitempty"`
	CreatedAt             time.Time                         `json:"createdAt"`
	UpdatedAt             time.Time                         `json:"updatedAt"`
}

type BindingRevision struct {
	ID           string                  `json:"id"`
	Revision     uint64                  `json:"revision"`
	DomainID     string                  `json:"domainId"`
	Subject      authorizationv1.Subject `json:"subject"`
	RoleID       string                  `json:"roleId"`
	RoleRevision uint64                  `json:"roleRevision"`
	NotBefore    time.Time               `json:"notBefore"`
	ExpiresAt    time.Time               `json:"expiresAt"`
	State        LifecycleState          `json:"state"`
	CreatedBy    string                  `json:"createdBy"`
	ApprovedBy   string                  `json:"approvedBy,omitempty"`
	CreatedAt    time.Time               `json:"createdAt"`
	UpdatedAt    time.Time               `json:"updatedAt"`
}

type AuditEvent struct {
	ID         string    `json:"id"`
	Action     string    `json:"action"`
	ObjectKind string    `json:"objectKind"`
	ObjectID   string    `json:"objectId"`
	Revision   uint64    `json:"revision"`
	SubjectID  string    `json:"subjectId"`
	Reason     string    `json:"reason,omitempty"`
	OccurredAt time.Time `json:"occurredAt"`
}

type State struct {
	Version            int                             `json:"version"`
	Generation         uint64                          `json:"generation"`
	PolicyRevision     uint64                          `json:"policyRevision"`
	RevocationRevision uint64                          `json:"revocationRevision"`
	Catalog            pluginv1.PermissionCatalog      `json:"catalog"`
	ProviderProfile    authorizationv1.ProviderProfile `json:"providerProfile"`
	Domains            []authorizationv1.PolicyDomain  `json:"domains"`
	Roles              []RoleRevision                  `json:"roles"`
	Bindings           []BindingRevision               `json:"bindings"`
	Revocations        []authorizationv1.Revocation    `json:"revocations"`
	Audit              []AuditEvent                    `json:"audit"`
	CurrentSnapshot    *authorizationv1.PolicySnapshot `json:"currentSnapshot,omitempty"`
}

type SnapshotPublication struct {
	Snapshot authorizationv1.SignedPolicySnapshot `json:"snapshot"`
	Digest   string                               `json:"digest"`
}
