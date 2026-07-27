// Package assessmentruntime implements the concrete scanner-facing workflow
// owned by the first-party Artifact Assessment Provider plugin.
package assessmentruntime

import (
	"context"
	"time"

	"cdsoft.com.cn/VastPlan/extensions/libraries/go/artifactassessment"
)

const (
	DefaultScannerID = "trivy.filesystem"
	MaxReportBytes   = int64(64 << 20)
)

type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityHigh     Severity = "high"
	SeverityMedium   Severity = "medium"
	SeverityLow      Severity = "low"
	SeverityUnknown  Severity = "unknown"
)

type LicenseDisposition string

const (
	LicenseAllowed LicenseDisposition = "allowed"
	LicenseDenied  LicenseDisposition = "denied"
	LicenseUnknown LicenseDisposition = "unknown"
)

type VulnerabilityFinding struct {
	ID       string
	Package  string
	Version  string
	Target   string
	Severity Severity
}

type LicenseFinding struct {
	Name        string
	Package     string
	Target      string
	Disposition LicenseDisposition
}

type EngineResult struct {
	Scanner         artifactassessment.Scanner
	Report          []byte
	Vulnerabilities []VulnerabilityFinding
	Licenses        []LicenseFinding
}

type Engine interface {
	Scan(context.Context, string) (EngineResult, error)
}

type Config struct {
	ProviderID string
	KeyID      string
	TTL        time.Duration
	Maximum    artifactassessment.MaximumFindings
}

type Evidence struct {
	Admission artifactassessment.AdmissionRecord
	Report    []byte
}

type StatusEvidence struct {
	Status artifactassessment.StatusRecord
	Report []byte
}

type StatusRequest struct {
	Scan            artifactassessment.ScanRequest
	AdmissionSHA256 string
	Sequence        uint64
	PreviousSHA256  string
}
