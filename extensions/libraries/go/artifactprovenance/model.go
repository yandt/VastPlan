// Package artifactprovenance defines the bounded, language-neutral trust
// contract between external provenance verifiers, repositories and Node Agents.
package artifactprovenance

import "time"

const (
	DSSEPayloadType     = "application/vnd.in-toto+json"
	InTotoStatementType = "https://in-toto.io/Statement/v1"
	SLSAProvenanceType  = "https://slsa.dev/provenance/v1"
	RecordSchemaVersion = "v1"
	MaxProvenanceBytes  = 2 << 20
	MaxRecordBytes      = 256 << 10
)

type Digest struct {
	Algorithm string `json:"algorithm"`
	Value     string `json:"value"`
}

type Source struct {
	URI     string   `json:"uri"`
	Digests []Digest `json:"digests"`
}

type StatementSummary struct {
	PredicateType string   `json:"predicateType"`
	BuilderID     string   `json:"builderId"`
	BuildType     string   `json:"buildType"`
	Sources       []Source `json:"sources"`
}

// VerificationRecord is signed by an external trusted Verifier Provider.
// Signature is over the same struct with Signature empty.
type VerificationRecord struct {
	SchemaVersion    string `json:"schemaVersion"`
	SubjectSHA256    string `json:"subjectSha256"`
	ProvenanceSHA256 string `json:"provenanceSha256"`
	StatementSummary
	Issuer     string    `json:"issuer,omitempty"`
	Workflow   string    `json:"workflow,omitempty"`
	ProviderID string    `json:"providerId"`
	KeyID      string    `json:"keyId"`
	PolicyID   string    `json:"policyId"`
	Algorithm  string    `json:"algorithm"`
	VerifiedAt time.Time `json:"verifiedAt"`
	ExpiresAt  time.Time `json:"expiresAt"`
	Signature  string    `json:"signature"`
}

type VerifierKey struct {
	ProviderID string     `json:"providerId"`
	KeyID      string     `json:"keyId"`
	PublicKey  string     `json:"publicKey"`
	NotBefore  *time.Time `json:"notBefore,omitempty"`
	NotAfter   *time.Time `json:"notAfter,omitempty"`
	Revoked    bool       `json:"revoked,omitempty"`
}

// Requirement selects one policy for a publisher/channel/plugin namespace.
// Empty publisher or pluginPrefix is a wildcard. Channel is always exact.
type Requirement struct {
	ID                  string   `json:"id"`
	Channel             string   `json:"channel"`
	Publisher           string   `json:"publisher,omitempty"`
	PluginPrefix        string   `json:"pluginPrefix,omitempty"`
	ProviderIDs         []string `json:"providerIds"`
	BuilderIDs          []string `json:"builderIds"`
	BuildTypes          []string `json:"buildTypes"`
	SourceURIPrefixes   []string `json:"sourceUriPrefixes"`
	Issuers             []string `json:"issuers,omitempty"`
	Workflows           []string `json:"workflows,omitempty"`
	RequireSourceDigest bool     `json:"requireSourceDigest"`
}

type TrustPolicy struct {
	RequiredChannels  []string      `json:"requiredChannels"`
	MaxRecordTTLHours int           `json:"maxRecordTtlHours,omitempty"`
	Keys              []VerifierKey `json:"keys"`
	Requirements      []Requirement `json:"requirements"`
}

type ArtifactIdentity struct {
	PluginID  string
	Channel   string
	Publisher string
	SHA256    string
}
