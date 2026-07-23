// Package artifactassessmentprovider implements the scanner-facing half of
// VastPlan's artifact assessment contract. It is provider SDK code and must
// never be imported by a kernel.
package artifactassessmentprovider

import (
	"context"
	"time"

	"cdsoft.com.cn/VastPlan/core/shared/go/artifactassessment"
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
