package authorizationpolicy

import (
	"time"

	authorizationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authorization/v1"
)

type ListRequest struct{}

type CreateRoleRequest struct {
	ExpectedGeneration uint64                            `json:"expectedGeneration"`
	ID                 string                            `json:"id"`
	DomainID           string                            `json:"domainId"`
	Title              string                            `json:"title"`
	Description        string                            `json:"description,omitempty"`
	Statements         []authorizationv1.PolicyStatement `json:"statements"`
}

type UpdateRoleRequest struct {
	ExpectedGeneration uint64                            `json:"expectedGeneration"`
	ID                 string                            `json:"id"`
	Revision           uint64                            `json:"revision"`
	Title              string                            `json:"title"`
	Description        string                            `json:"description,omitempty"`
	Statements         []authorizationv1.PolicyStatement `json:"statements"`
}

type CreateBindingRequest struct {
	ExpectedGeneration uint64                  `json:"expectedGeneration"`
	ID                 string                  `json:"id"`
	DomainID           string                  `json:"domainId"`
	Subject            authorizationv1.Subject `json:"subject"`
	RoleID             string                  `json:"roleId"`
	RoleRevision       uint64                  `json:"roleRevision"`
	NotBefore          time.Time               `json:"notBefore"`
	ExpiresAt          time.Time               `json:"expiresAt"`
}

type UpdateBindingRequest struct {
	ExpectedGeneration uint64                  `json:"expectedGeneration"`
	ID                 string                  `json:"id"`
	Revision           uint64                  `json:"revision"`
	DomainID           string                  `json:"domainId"`
	Subject            authorizationv1.Subject `json:"subject"`
	RoleID             string                  `json:"roleId"`
	RoleRevision       uint64                  `json:"roleRevision"`
	NotBefore          time.Time               `json:"notBefore"`
	ExpiresAt          time.Time               `json:"expiresAt"`
}

type TransitionRequest struct {
	ExpectedGeneration uint64 `json:"expectedGeneration"`
	ID                 string `json:"id"`
	Revision           uint64 `json:"revision"`
	Reason             string `json:"reason,omitempty"`
}

type RevokeRequest struct {
	ExpectedGeneration uint64    `json:"expectedGeneration"`
	ID                 string    `json:"id"`
	Kind               string    `json:"kind"`
	TargetID           string    `json:"targetId"`
	EffectiveAt        time.Time `json:"effectiveAt"`
	ReasonCode         string    `json:"reasonCode"`
}

type PublishSnapshotRequest struct {
	ExpectedGeneration uint64   `json:"expectedGeneration"`
	Audience           []string `json:"audience"`
	TTLSeconds         int64    `json:"ttlSeconds"`
	Reason             string   `json:"reason"`
}
