// Package artifactassessment defines the language-neutral security assessment
// contract shared by scanner Providers, artifact repositories and trusted hosts.
package artifactassessment

import (
	"context"
	"time"
)

const (
	SchemaVersion  = "v1"
	MaxRecordBytes = 256 << 10
	MaxClockSkew   = 5 * time.Minute
	DecisionPass   = "pass"
	DecisionFail   = "fail"
)

// Scanner identifies the external engine and the exact advisory database used
// for an evaluation. The kernel deliberately does not know vendor-specific
// scanner concepts.
type Scanner struct {
	ID               string `json:"id"`
	Version          string `json:"version"`
	DatabaseRevision string `json:"databaseRevision"`
}

type VulnerabilitySummary struct {
	Critical     uint64 `json:"critical"`
	High         uint64 `json:"high"`
	Medium       uint64 `json:"medium"`
	Low          uint64 `json:"low"`
	Unknown      uint64 `json:"unknown"`
	ReportSHA256 string `json:"reportSha256"`
}

type LicenseSummary struct {
	Allowed      uint64 `json:"allowed"`
	Denied       uint64 `json:"denied"`
	Unknown      uint64 `json:"unknown"`
	ReportSHA256 string `json:"reportSha256"`
}

// Evaluation is the normalized result. Detailed scanner-native reports remain
// external evidence and are content-bound through their SHA-256 digests.
type Evaluation struct {
	SubjectSHA256   string               `json:"subjectSha256"`
	SBOMSHA256      string               `json:"sbomSha256"`
	Scanner         Scanner              `json:"scanner"`
	Vulnerabilities VulnerabilitySummary `json:"vulnerabilities"`
	Licenses        LicenseSummary       `json:"licenses"`
	Decision        string               `json:"decision"`
	EvaluatedAt     time.Time            `json:"evaluatedAt"`
	ExpiresAt       time.Time            `json:"expiresAt"`
}

// AdmissionRecord is immutable release evidence. Signature covers the record
// with Signature empty.
type AdmissionRecord struct {
	SchemaVersion string     `json:"schemaVersion"`
	Evaluation    Evaluation `json:"evaluation"`
	ProviderID    string     `json:"providerId"`
	KeyID         string     `json:"keyId"`
	PolicyID      string     `json:"policyId"`
	Algorithm     string     `json:"algorithm"`
	Signature     string     `json:"signature"`
}

// StatusRecord is an append-only rescan result. Sequence and PreviousSHA256
// prevent rollback or replacement with an older otherwise-valid scan.
type StatusRecord struct {
	SchemaVersion   string     `json:"schemaVersion"`
	AdmissionSHA256 string     `json:"admissionSha256"`
	Sequence        uint64     `json:"sequence"`
	PreviousSHA256  string     `json:"previousSha256"`
	Evaluation      Evaluation `json:"evaluation"`
	ProviderID      string     `json:"providerId"`
	KeyID           string     `json:"keyId"`
	PolicyID        string     `json:"policyId"`
	Algorithm       string     `json:"algorithm"`
	Signature       string     `json:"signature"`
}

type ProviderKey struct {
	ProviderID string     `json:"providerId"`
	KeyID      string     `json:"keyId"`
	PublicKey  string     `json:"publicKey"`
	NotBefore  *time.Time `json:"notBefore,omitempty"`
	NotAfter   *time.Time `json:"notAfter,omitempty"`
	Revoked    bool       `json:"revoked,omitempty"`
}

// MaximumFindings uses pointers so an omitted threshold means "not governed";
// an explicit zero means the category must be clean.
type MaximumFindings struct {
	Critical             *uint64 `json:"critical,omitempty"`
	High                 *uint64 `json:"high,omitempty"`
	Medium               *uint64 `json:"medium,omitempty"`
	Low                  *uint64 `json:"low,omitempty"`
	UnknownVulnerability *uint64 `json:"unknownVulnerability,omitempty"`
	DeniedLicense        *uint64 `json:"deniedLicense,omitempty"`
	UnknownLicense       *uint64 `json:"unknownLicense,omitempty"`
}

type Requirement struct {
	ID                   string          `json:"id"`
	Channel              string          `json:"channel"`
	Publisher            string          `json:"publisher,omitempty"`
	PluginPrefix         string          `json:"pluginPrefix,omitempty"`
	ProviderIDs          []string        `json:"providerIds"`
	ScannerIDs           []string        `json:"scannerIds"`
	Maximum              MaximumFindings `json:"maximum"`
	RequireReportDigests bool            `json:"requireReportDigests"`
}

type TrustPolicy struct {
	RequiredChannels  []string      `json:"requiredChannels"`
	MaxRecordTTLHours int           `json:"maxRecordTtlHours,omitempty"`
	Keys              []ProviderKey `json:"keys"`
	Requirements      []Requirement `json:"requirements"`
}

type ArtifactIdentity struct {
	PluginID   string
	Channel    string
	Publisher  string
	SHA256     string
	SBOMSHA256 string
}

// ScanRequest is intentionally report-format agnostic. A Provider can invoke
// Trivy, Grype, OSV-Scanner, ScanCode or an enterprise service and normalize the
// result without leaking that choice into the kernel.
type ScanRequest struct {
	Identity ArtifactIdentity
	Package  []byte
	SBOM     []byte
	PolicyID string
}

type Provider interface {
	Assess(context.Context, ScanRequest) (AdmissionRecord, error)
}
