package deploymentmanager

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"cdsoft.com.cn/VastPlan/core/shared/go/deploymentpublication"
	"cdsoft.com.cn/VastPlan/core/shared/go/platformprofileactivation"
)

type profileActivationRecord struct {
	platformprofileactivation.Activation
	Request         platformprofileactivation.CreateActivationRequest `json:"request"`
	Prepare         platformprofileactivation.PrepareRequest          `json:"prepare"`
	RequestDigest   string                                            `json:"requestDigest"`
	CandidateStatus platformprofileactivation.Status                  `json:"candidateStatus,omitempty"`
	Preview         deploymentpublication.Result                      `json:"preview"`
	CreatedAt       string                                            `json:"createdAt"`
	UpdatedAt       string                                            `json:"updatedAt"`
}

func (record profileActivationRecord) validate(tenantID string) error {
	if record.Activation.Validate() != nil || record.Request.Validate() != nil || record.CandidateID != record.Request.CandidateID ||
		record.ConfigurationID != record.Request.ConfigurationID || record.Prepare.CandidateID != record.CandidateID ||
		record.Prepare.ConfigurationID != record.ConfigurationID || record.Prepare.Composition.Metadata.Tenant != tenantID ||
		record.Prepare.Composition.Metadata.Name != record.Deployment || record.Prepare.DeploymentRevision != record.DeploymentRevision ||
		strings.TrimSpace(record.CreatedAt) == "" || strings.TrimSpace(record.UpdatedAt) == "" {
		return errors.New("Platform Profile 激活状态身份无效")
	}
	createdAt, createdErr := time.Parse(time.RFC3339Nano, record.CreatedAt)
	updatedAt, updatedErr := time.Parse(time.RFC3339Nano, record.UpdatedAt)
	if createdErr != nil || updatedErr != nil || updatedAt.Before(createdAt) {
		return errors.New("Platform Profile 激活状态时间无效")
	}
	expectedRequest := platformprofileactivation.CreateActivationRequest{
		CandidateID: record.Prepare.CandidateID, ConfigurationID: record.Prepare.ConfigurationID,
		ConfigCatalogDigest: record.Prepare.ConfigCatalogDigest, SchemaDigest: record.Prepare.SchemaDigest,
		ArtifactSHA256: record.Prepare.ArtifactSHA256, Values: record.Prepare.Values, Credentials: record.Prepare.Credentials,
	}
	if profileActivationSubmissionHash(record.Request) != profileActivationSubmissionHash(expectedRequest) {
		return errors.New("Platform Profile 激活外部请求与可信准备请求不一致")
	}
	digest, err := platformprofileactivation.DigestPrepareRequest(record.Prepare)
	if err != nil || digest != record.RequestDigest {
		return errors.New("Platform Profile 激活请求摘要无效")
	}
	if record.Status != platformprofileactivation.ActivationPreparing {
		if record.CandidateStatus == "" || record.Preview.Digest == "" || record.Preview.Deployment.Revision != record.DeploymentRevision || record.Preview.ConfigurationCatalog.Digest == "" {
			return errors.New("Platform Profile 激活缺少可信候选预览")
		}
	}
	return nil
}

func cloneProfileActivation(record profileActivationRecord) profileActivationRecord {
	raw, _ := json.Marshal(record)
	var out profileActivationRecord
	_ = json.Unmarshal(raw, &out)
	return out
}

func publicProfileActivation(record profileActivationRecord) platformprofileactivation.Activation {
	return record.Activation
}

func profileActivationTerminal(status platformprofileactivation.ActivationStatus) bool {
	switch status {
	case platformprofileactivation.ActivationReady, platformprofileactivation.ActivationRolledBack,
		platformprofileactivation.ActivationAborted, platformprofileactivation.ActivationFailed:
		return true
	default:
		return false
	}
}

func profileActivationLocksDeployment(state *tenantState, deployment string) bool {
	for _, record := range state.ProfileActivations {
		if record.Deployment == deployment && !profileActivationTerminal(record.Status) {
			return true
		}
	}
	return false
}
