package artifactassessment

import (
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strings"
	"time"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	semver "github.com/Masterminds/semver/v3"
)

var scanLeaseIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{0,199}$`)
var scanLeaseChannelPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

const (
	AssessmentProviderPluginID   = "cn.vastplan.platform.artifacts.assessment.provider"
	AssessmentControllerPluginID = "cn.vastplan.platform.artifacts.assessment.controller"
	ScanLeaseTTL                 = 30 * time.Second
)

// ScanLeaseRequest binds a one-time artifact data-plane read to the exact
// identity already selected by a trusted rescan/admission workflow.
type ScanLeaseRequest struct {
	Ref           pluginv1.ArtifactRef `json:"ref"`
	SubjectSHA256 string               `json:"subjectSha256"`
	SBOMSHA256    string               `json:"sbomSha256"`
}

type ProviderAssessmentRequest struct {
	ScanLeaseRequest
	PolicyID string `json:"policyId"`
}

type ProviderStatusRequest struct {
	ProviderAssessmentRequest
	AdmissionSHA256 string `json:"admissionSha256"`
	Sequence        uint64 `json:"sequence"`
	PreviousSHA256  string `json:"previousSha256"`
}

type AppendStatusRequest struct {
	Ref    pluginv1.ArtifactRef `json:"ref"`
	Record json.RawMessage      `json:"record"`
}

type ProviderRuntimeStatus struct {
	SchemaVersion      string  `json:"schemaVersion"`
	Scanner            Scanner `json:"scanner"`
	AssessmentRevision string  `json:"assessmentRevision"`
}

func ValidateProviderRuntimeStatus(value ProviderRuntimeStatus) error {
	if value.SchemaVersion != SchemaVersion || value.Scanner.ID == "" || value.Scanner.Version == "" || value.Scanner.DatabaseRevision == "" || !validSHA256(value.AssessmentRevision) {
		return errors.New("安全评估 Provider runtime status 无效")
	}
	return nil
}

func ValidateProviderAssessmentRequest(value ProviderAssessmentRequest) error {
	if ValidateScanLeaseRequest(value.ScanLeaseRequest) != nil || value.PolicyID == "" || strings.TrimSpace(value.PolicyID) != value.PolicyID || len(value.PolicyID) > 160 {
		return errors.New("安全评估 Provider 请求无效")
	}
	return nil
}

func ValidateProviderStatusRequest(value ProviderStatusRequest) error {
	if ValidateProviderAssessmentRequest(value.ProviderAssessmentRequest) != nil || value.Sequence == 0 || !validSHA256(value.AdmissionSHA256) || !validSHA256(value.PreviousSHA256) {
		return errors.New("安全复扫 Provider 请求无效")
	}
	return nil
}

// ScanLease carries a secret-bearing URL. It must never be logged, persisted
// in controller state, returned to Portal, or reused after one GET.
type ScanLease struct {
	SchemaVersion string               `json:"schemaVersion"`
	Ref           pluginv1.ArtifactRef `json:"ref"`
	SubjectSHA256 string               `json:"subjectSha256"`
	SBOMSHA256    string               `json:"sbomSha256"`
	Audience      string               `json:"audience"`
	URL           string               `json:"url"`
	ExpiresAt     time.Time            `json:"expiresAt"`
}

func ValidateScanLeaseRequest(value ScanLeaseRequest) error {
	if !scanLeaseIDPattern.MatchString(value.Ref.PluginID) || !scanLeaseChannelPattern.MatchString(value.Ref.Channel) || !validSHA256(value.SubjectSHA256) || !validSHA256(value.SBOMSHA256) {
		return errors.New("安全评估扫描租约请求无效")
	}
	if _, err := semver.StrictNewVersion(value.Ref.Version); err != nil {
		return errors.New("安全评估扫描租约版本不是严格 SemVer")
	}
	return nil
}

func ValidateScanLease(value ScanLease, audience string, now time.Time) error {
	if value.SchemaVersion != SchemaVersion || ValidateScanLeaseRequest(ScanLeaseRequest{Ref: value.Ref, SubjectSHA256: value.SubjectSHA256, SBOMSHA256: value.SBOMSHA256}) != nil ||
		value.Audience != audience || audience == "" || value.ExpiresAt.Location() != time.UTC || !value.ExpiresAt.After(now) || value.ExpiresAt.Sub(now) > ScanLeaseTTL+MaxClockSkew {
		return errors.New("安全评估扫描租约 claims 无效")
	}
	parsed, err := url.Parse(value.URL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" || parsed.RawQuery == "" {
		return errors.New("安全评估扫描租约 URL 无效")
	}
	query := parsed.Query()
	tickets := query["vp_ticket"]
	if len(query) != 1 || len(tickets) != 1 || len(tickets[0]) != 43 || strings.ContainsAny(tickets[0], "+/=") {
		return errors.New("安全评估扫描租约 URL ticket 无效")
	}
	return nil
}
