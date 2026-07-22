package broker

import (
	"time"

	authenticationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authentication/v1"
)

const managementStateVersion = 1

type ManagedProvider struct {
	Profile    authenticationv1.AuthenticationProviderProfile   `json:"profile"`
	Lifecycle  authenticationv1.AuthenticationProviderLifecycle `json:"lifecycle"`
	TestedBy   string                                           `json:"testedBy,omitempty"`
	ApprovedBy string                                           `json:"approvedBy,omitempty"`
}

type ManagementState struct {
	Version       int                                             `json:"version"`
	Generation    uint64                                          `json:"generation"`
	Providers     []ManagedProvider                               `json:"providers"`
	Catalog       *authenticationv1.AuthenticationProviderCatalog `json:"catalog,omitempty"`
	AccessCatalog *authenticationv1.AccessProfileCatalog          `json:"accessCatalog,omitempty"`
	UpdatedAt     time.Time                                       `json:"updatedAt"`
}

type CreateDraftRequest struct {
	ExpectedGeneration uint64                                         `json:"expectedGeneration"`
	Profile            authenticationv1.AuthenticationProviderProfile `json:"profile"`
}

type ProviderActionRequest struct {
	ExpectedGeneration uint64 `json:"expectedGeneration"`
	ProviderID         string `json:"providerId"`
}

type RecordTestRequest struct {
	ExpectedGeneration uint64                                         `json:"expectedGeneration"`
	ProviderID         string                                         `json:"providerId"`
	Actor              string                                         `json:"actor"`
	Assertion          authenticationv1.SignedAuthenticationAssertion `json:"assertion"`
}

type ApproveRequest struct {
	ExpectedGeneration uint64 `json:"expectedGeneration"`
	ProviderID         string `json:"providerId"`
	Actor              string `json:"actor"`
}

type PublishRequest struct {
	ExpectedGeneration uint64                                `json:"expectedGeneration"`
	CatalogID          string                                `json:"catalogId"`
	CatalogRevision    uint64                                `json:"catalogRevision"`
	Bindings           []authenticationv1.ProviderBinding    `json:"bindings"`
	AccessCatalog      authenticationv1.AccessProfileCatalog `json:"accessCatalog"`
}
