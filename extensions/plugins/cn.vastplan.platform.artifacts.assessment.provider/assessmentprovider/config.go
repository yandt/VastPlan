// Package assessmentprovider orchestrates leased artifact download, isolated
// scanning, short-lived signing, and content-addressed report archival.
package assessmentprovider

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"time"

	commonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/common/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifactassessment"
	"cdsoft.com.cn/VastPlan/core/shared/go/credentiallease"
)

const (
	PluginID       = artifactassessment.AssessmentProviderPluginID
	PluginVersion  = "0.3.0"
	Capability     = "platform.artifacts.assessment"
	SigningPurpose = "artifact.assessment.signing-key"
)

type Config struct {
	ProviderID             string                             `json:"providerId"`
	KeyID                  string                             `json:"keyId"`
	SigningKeyRef          commonv1.ManagedCredentialRef      `json:"signingKeyRef"`
	TrivyBinary            string                             `json:"trivyBinary"`
	TrivySnapshotDirectory string                             `json:"trivySnapshotDirectory"`
	ScannerVersion         string                             `json:"scannerVersion"`
	DatabaseRevision       string                             `json:"databaseRevision"`
	WorkRoot               string                             `json:"workRoot"`
	ReportArchiveDirectory string                             `json:"reportArchiveDirectory"`
	TTLHours               int                                `json:"ttlHours"`
	TimeoutSeconds         int                                `json:"timeoutSeconds"`
	AllowedLicenses        []string                           `json:"allowedLicenses"`
	FullLicenseScan        bool                               `json:"fullLicenseScan"`
	Maximum                artifactassessment.MaximumFindings `json:"maximum"`
}

func (c Config) Validate() error {
	for _, path := range []string{c.TrivyBinary, c.TrivySnapshotDirectory, c.WorkRoot, c.ReportArchiveDirectory} {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return errors.New("Assessment Provider 路径必须是规范绝对路径")
		}
	}
	if filepath.Base(c.TrivySnapshotDirectory) != c.DatabaseRevision || filepath.Base(filepath.Dir(c.TrivySnapshotDirectory)) != "snapshots" {
		return errors.New("Assessment Provider Trivy snapshot 路径必须精确绑定 databaseRevision")
	}
	if c.SigningKeyRef.Owner != PluginID || c.SigningKeyRef.Purpose != SigningPurpose || c.SigningKeyRef.Scope != "tenant" || c.SigningKeyRef.Name != "" || credentiallease.ValidateCredentialRef(c.SigningKeyRef) != nil {
		return errors.New("Assessment Provider signingKeyRef owner/purpose 无效")
	}
	if c.TTLHours < 1 || c.TTLHours > 8760 || c.TimeoutSeconds < 1 || c.TimeoutSeconds > 7200 {
		return errors.New("Assessment Provider TTL 或 timeout 无效")
	}
	return nil
}

func (c Config) TTL() time.Duration     { return time.Duration(c.TTLHours) * time.Hour }
func (c Config) Timeout() time.Duration { return time.Duration(c.TimeoutSeconds) * time.Second }

func (c Config) AssessmentRevision() string {
	raw, _ := json.Marshal(struct {
		ScannerVersion   string                             `json:"scannerVersion"`
		DatabaseRevision string                             `json:"databaseRevision"`
		AllowedLicenses  []string                           `json:"allowedLicenses"`
		FullLicenseScan  bool                               `json:"fullLicenseScan"`
		Maximum          artifactassessment.MaximumFindings `json:"maximum"`
		TTLHours         int                                `json:"ttlHours"`
	}{c.ScannerVersion, c.DatabaseRevision, c.AllowedLicenses, c.FullLicenseScan, c.Maximum, c.TTLHours})
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}
