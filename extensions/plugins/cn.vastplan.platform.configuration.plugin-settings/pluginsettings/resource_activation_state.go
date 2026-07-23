package pluginsettings

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	configurationresourcev1 "cdsoft.com.cn/VastPlan/contracts/schemas/configurationresource/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfiguration"
)

type resourceActivationStatus string

const (
	resourcePreparing       resourceActivationStatus = "Preparing"
	resourcePendingApproval resourceActivationStatus = "PendingApproval"
	resourceApproved        resourceActivationStatus = "Approved"
	resourceActivating      resourceActivationStatus = "Activating"
	resourceAborting        resourceActivationStatus = "Aborting"
	resourceReady           resourceActivationStatus = "Ready"
	resourceAborted         resourceActivationStatus = "Aborted"
	resourceFailed          resourceActivationStatus = "Failed"
)

type resourceActivationRecord struct {
	Target        pluginconfiguration.ControllerTarget   `json:"target"`
	Prepare       configurationresourcev1.PrepareRequest `json:"prepare"`
	RequestDigest string                                 `json:"requestDigest"`
	Status        resourceActivationStatus               `json:"status"`
	SubmittedBy   string                                 `json:"submittedBy"`
	ApprovedBy    string                                 `json:"approvedBy,omitempty"`
	CreatedAt     string                                 `json:"createdAt"`
	UpdatedAt     string                                 `json:"updatedAt"`
}

func (record resourceActivationRecord) validate(candidate pluginconfiguration.Candidate, tenant string) error {
	if strings.TrimSpace(tenant) == "" || candidate.ApplyPath != pluginconfiguration.ApplyResourceProfile ||
		record.Target.Protocol != configurationresourcev1.Protocol || record.Target.ExtensionPoint != configurationresourcev1.ExtensionPoint ||
		strings.TrimSpace(record.Target.Capability) == "" || strings.TrimSpace(record.Target.LogicalService) == "" ||
		record.Prepare.CandidateID != candidate.ID || record.Prepare.ConfigurationID != candidate.ConfigurationID ||
		record.Prepare.CollectionID != candidate.ResourceCollectionID || record.Prepare.ResourceID != candidate.ResourceID ||
		string(record.Prepare.Action) != candidate.ResourceAction || record.Prepare.CatalogDigest != candidate.CatalogDigest ||
		record.Prepare.SchemaDigest != candidate.SchemaDigest || record.Prepare.ArtifactSHA256 != candidate.ArtifactSHA256 ||
		!sameOptionalJSON(record.Prepare.Values, candidate.Values) || strings.TrimSpace(record.SubmittedBy) == "" {
		return errors.New("resource activation 身份或候选绑定无效")
	}
	expectedCandidateStatus, expectedExternalStatus, ok := resourceCandidateProjection(record.Status)
	if !ok || candidate.Status != expectedCandidateStatus || candidate.ExternalStatus != expectedExternalStatus {
		return errors.New("resource activation 与公开候选状态不一致")
	}
	digest, err := configurationresourcev1.DigestPrepareRequest(record.Prepare)
	if err != nil || digest != record.RequestDigest {
		return errors.New("resource activation 请求摘要无效")
	}
	createdAt, createdErr := time.Parse(time.RFC3339Nano, record.CreatedAt)
	updatedAt, updatedErr := time.Parse(time.RFC3339Nano, record.UpdatedAt)
	if createdErr != nil || updatedErr != nil || updatedAt.Before(createdAt) {
		return errors.New("resource activation 时间无效")
	}
	switch record.Status {
	case resourcePreparing, resourcePendingApproval:
		if record.ApprovedBy != "" {
			return errors.New("未审批 resource activation 携带 approver")
		}
	case resourceApproved, resourceActivating, resourceAborting, resourceReady:
		if strings.TrimSpace(record.ApprovedBy) == "" || record.ApprovedBy == record.SubmittedBy {
			return errors.New("resource activation 未满足异人审批")
		}
	case resourceAborted, resourceFailed:
		if record.ApprovedBy != "" && record.ApprovedBy == record.SubmittedBy {
			return errors.New("终止 resource activation 审批身份无效")
		}
	default:
		return errors.New("resource activation 状态无效")
	}
	return nil
}

func resourceCandidateProjection(status resourceActivationStatus) (pluginconfiguration.CandidateStatus, string, bool) {
	switch status {
	case resourcePreparing:
		return pluginconfiguration.CandidatePublishing, string(resourcePreparing), true
	case resourcePendingApproval:
		return pluginconfiguration.CandidatePublishing, string(configurationresourcev1.StatusPrepared), true
	case resourceApproved:
		return pluginconfiguration.CandidatePublishing, string(resourceApproved), true
	case resourceActivating:
		return pluginconfiguration.CandidateActivating, string(resourceActivating), true
	case resourceAborting:
		return pluginconfiguration.CandidateRollingBack, string(resourceAborting), true
	case resourceReady:
		return pluginconfiguration.CandidateReady, string(configurationresourcev1.StatusCommitted), true
	case resourceAborted:
		return pluginconfiguration.CandidateRolledBack, string(configurationresourcev1.StatusAborted), true
	case resourceFailed:
		return pluginconfiguration.CandidateFailed, string(resourceFailed), true
	default:
		return "", "", false
	}
}

func sameOptionalJSON(left, right []byte) bool {
	if len(left) == 0 || len(right) == 0 {
		return len(left) == 0 && len(right) == 0
	}
	return sameJSON(left, right)
}

func cloneResourceActivation(record resourceActivationRecord) resourceActivationRecord {
	raw, _ := json.Marshal(record)
	var clone resourceActivationRecord
	_ = json.Unmarshal(raw, &clone)
	return clone
}
