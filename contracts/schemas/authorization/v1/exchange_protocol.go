package authorizationv1

import "encoding/json"

type ExchangeDocument struct {
	Format        string          `json:"format"`
	SchemaVersion string          `json:"schemaVersion"`
	Digest        string          `json:"digest"`
	Content       json.RawMessage `json:"content"`
}

type ExchangePlanImportRequest struct {
	DomainID string           `json:"domainId"`
	Document ExchangeDocument `json:"document"`
}

type ImportSummary struct {
	Roles    int `json:"roles"`
	Bindings int `json:"bindings"`
	Warnings int `json:"warnings"`
}

type ExchangePlanImportResult struct {
	PlanID         string        `json:"planId"`
	SourceDigest   string        `json:"sourceDigest"`
	ProposalDigest string        `json:"proposalDigest"`
	Summary        ImportSummary `json:"summary"`
}

type ExchangeValidateRequest struct {
	PlanID         string `json:"planId"`
	ProposalDigest string `json:"proposalDigest"`
}

type Diagnostic struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Path     string `json:"path,omitempty"`
	Message  string `json:"message"`
}

type ExchangeValidateResult struct {
	Valid       bool         `json:"valid"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// Import returns a proposal only. The authorization-policy plugin must create
// a Draft revision and run approval/CAS/audit; an Exchange Provider cannot
// publish or write the Store directly.
type ExchangeImportRequest struct {
	PlanID         string `json:"planId"`
	ProposalDigest string `json:"proposalDigest"`
}

type PolicyImportProposal struct {
	DomainID     string           `json:"domainId"`
	SourceDigest string           `json:"sourceDigest"`
	Roles        []CompiledRole   `json:"roles"`
	Bindings     []SubjectBinding `json:"bindings"`
}

type ExchangeImportResult struct {
	Proposal PolicyImportProposal `json:"proposal"`
}

type ExchangeExportRequest struct {
	DomainID string               `json:"domainId"`
	Format   string               `json:"format"`
	Snapshot SignedPolicySnapshot `json:"snapshot"`
}

type ExchangeExportResult struct {
	Document ExchangeDocument `json:"document"`
}
