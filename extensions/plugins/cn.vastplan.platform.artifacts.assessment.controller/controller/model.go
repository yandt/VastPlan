package controller

import (
	"encoding/json"
	"errors"
	"time"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifactassessment"
)

const planSchemaVersion = "v1"

type Plan struct {
	SchemaVersion      string               `json:"schemaVersion"`
	Ref                pluginv1.ArtifactRef `json:"ref"`
	SubjectSHA256      string               `json:"subjectSha256"`
	SBOMSHA256         string               `json:"sbomSha256"`
	AdmissionSHA256    string               `json:"admissionSha256"`
	PolicyID           string               `json:"policyId"`
	LastSequence       uint64               `json:"lastSequence"`
	LastRecordSHA256   string               `json:"lastRecordSha256"`
	DatabaseRevision   string               `json:"databaseRevision,omitempty"`
	AssessmentRevision string               `json:"assessmentRevision,omitempty"`
	NextScanAt         time.Time            `json:"nextScanAt"`
	Attempts           uint32               `json:"attempts"`
	LastErrorCode      string               `json:"lastErrorCode,omitempty"`
	PendingRecord      json.RawMessage      `json:"pendingRecord,omitempty"`
	UpdatedAt          time.Time            `json:"updatedAt"`
}

func (p Plan) validate() error {
	if p.SchemaVersion != planSchemaVersion || artifactassessment.ValidateScanLeaseRequest(artifactassessment.ScanLeaseRequest{Ref: p.Ref, SubjectSHA256: p.SubjectSHA256, SBOMSHA256: p.SBOMSHA256}) != nil || len(p.AdmissionSHA256) != 64 || p.PolicyID == "" || p.NextScanAt.IsZero() || p.NextScanAt.Location() != time.UTC || p.UpdatedAt.IsZero() || p.UpdatedAt.Location() != time.UTC {
		return errors.New("Assessment Controller plan 无效")
	}
	if p.LastSequence == 0 && p.LastRecordSHA256 != p.AdmissionSHA256 || p.LastSequence > 0 && len(p.LastRecordSHA256) != 64 {
		return errors.New("Assessment Controller plan 链头无效")
	}
	if len(p.PendingRecord) != 0 {
		if _, _, err := artifactassessment.InspectStatus(p.PendingRecord); err != nil {
			return err
		}
	}
	return nil
}

type Stats struct {
	LastRunAt       time.Time `json:"lastRunAt,omitempty"`
	CatalogRevision uint64    `json:"catalogRevision"`
	Eligible        int       `json:"eligible"`
	Deferred        int       `json:"deferred"`
	Succeeded       int       `json:"succeeded"`
	Failed          int       `json:"failed"`
	Conflicts       int       `json:"conflicts"`
}
