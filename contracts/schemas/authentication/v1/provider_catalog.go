package authenticationv1

import (
	"strings"
	"time"

	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
)

// ProviderPurpose describes a protocol surface offered by an enterprise
// identity Provider. It deliberately does not describe users, roles or groups.
type ProviderPurpose string

const (
	PurposePortalLogin       ProviderPurpose = "portal-login"
	PurposeMobileToken       ProviderPurpose = "mobile-token"
	PurposeRunnerToken       ProviderPurpose = "runner-token"
	PurposeTokenVerification ProviderPurpose = "token-verification"
	PurposeDirectory         ProviderPurpose = "directory"
	PurposeProvisioning      ProviderPurpose = "provisioning"
)

type ProviderLifecycleState string

const (
	ProviderDraft     ProviderLifecycleState = "draft"
	ProviderValidated ProviderLifecycleState = "validated"
	ProviderTested    ProviderLifecycleState = "tested"
	ProviderApproved  ProviderLifecycleState = "approved"
	ProviderPublished ProviderLifecycleState = "published"
	ProviderRetired   ProviderLifecycleState = "retired"
)

type ProviderReadiness string

const (
	ProviderUnknown  ProviderReadiness = "unknown"
	ProviderBlocked  ProviderReadiness = "blocked"
	ProviderReady    ProviderReadiness = "ready"
	ProviderDegraded ProviderReadiness = "degraded"
	ProviderFailed   ProviderReadiness = "failed"
)

// AuthenticationProviderProfile is an immutable operator-approved instance of
// a Provider contribution. Configuration points to a separate document so the
// profile and catalog can never contain client secrets, passwords or tokens.
type AuthenticationProviderProfile struct {
	compositioncommonv1.Document
	ContributionID       string                  `json:"contributionId"`
	Configuration        compositioncommonv1.Ref `json:"configuration"`
	Purposes             []ProviderPurpose       `json:"purposes"`
	Methods              []string                `json:"methods"`
	SubjectNamespace     string                  `json:"subjectNamespace"`
	RequiredCapabilities []string                `json:"requiredCapabilities"`
}

// AuthenticationProviderLifecycle records operator workflow and runtime
// readiness separately. A published profile can become temporarily blocked
// without rewriting the immutable profile or losing its approval history.
type AuthenticationProviderLifecycle struct {
	SchemaVersion     string                  `json:"schemaVersion"`
	Profile           compositioncommonv1.Ref `json:"profile"`
	State             ProviderLifecycleState  `json:"state"`
	Readiness         ProviderReadiness       `json:"readiness"`
	UnmetCapabilities []string                `json:"unmetCapabilities"`
	UpdatedAt         time.Time               `json:"updatedAt"`
	TestedAt          *time.Time              `json:"testedAt,omitempty"`
	ApprovedAt        *time.Time              `json:"approvedAt,omitempty"`
	PublishedAt       *time.Time              `json:"publishedAt,omitempty"`
}

// ProviderCatalogEntry is a safe projection of one published profile. It is
// intentionally duplicated into the signed catalog so routing never depends on
// mutable plugin configuration at request time.
type ProviderCatalogEntry struct {
	Profile              compositioncommonv1.Ref `json:"profile"`
	ContributionID       string                  `json:"contributionId"`
	Purposes             []ProviderPurpose       `json:"purposes"`
	Methods              []string                `json:"methods"`
	SubjectNamespace     string                  `json:"subjectNamespace"`
	RequiredCapabilities []string                `json:"requiredCapabilities"`
}

type ProviderBinding struct {
	TenantID         string   `json:"tenantId"`
	PortalID         string   `json:"portalId"`
	DefaultProvider  string   `json:"defaultProvider"`
	AllowedProviders []string `json:"allowedProviders"`
}

type AuthenticationProviderCatalog struct {
	compositioncommonv1.Document
	Providers []ProviderCatalogEntry `json:"providers"`
	Bindings  []ProviderBinding      `json:"bindings"`
}

// Resolve returns the only provider authorized for a method on the selected
// tenant/Portal. Catalog validation rejects ambiguous bindings in advance.
func (catalog AuthenticationProviderCatalog) Resolve(tenantID, portalID, methodID string) (ProviderCatalogEntry, bool) {
	for _, binding := range catalog.Bindings {
		if binding.TenantID != tenantID || binding.PortalID != portalID {
			continue
		}
		allowed := make(map[string]struct{}, len(binding.AllowedProviders))
		for _, providerID := range binding.AllowedProviders {
			allowed[providerID] = struct{}{}
		}
		for _, provider := range catalog.Providers {
			if _, ok := allowed[provider.Profile.ID]; ok && containsString(provider.Methods, methodID) {
				return provider, true
			}
		}
	}
	return ProviderCatalogEntry{}, false
}

// StableSubjectKey prevents equal external subject values from colliding
// across Provider profiles or issuers. It contains identity only, never policy.
func StableSubjectKey(providerProfileID, issuer, subject string) string {
	return providerProfileID + "\x00" + strings.TrimSpace(issuer) + "\x00" + subject
}

// CanTransitionProvider is the single lifecycle transition table shared by
// management APIs, controllers and Provider implementations.
func CanTransitionProvider(from, to ProviderLifecycleState) bool {
	if from == to {
		return true
	}
	allowed := map[ProviderLifecycleState][]ProviderLifecycleState{
		ProviderDraft:     {ProviderValidated, ProviderRetired},
		ProviderValidated: {ProviderDraft, ProviderTested, ProviderRetired},
		ProviderTested:    {ProviderValidated, ProviderApproved, ProviderRetired},
		ProviderApproved:  {ProviderTested, ProviderPublished, ProviderRetired},
		ProviderPublished: {ProviderApproved, ProviderRetired},
		ProviderRetired:   {ProviderDraft},
	}
	for _, candidate := range allowed[from] {
		if candidate == to {
			return true
		}
	}
	return false
}
