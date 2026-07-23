package pluginsettings

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"time"

	configurationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/configuration/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfiguration"
)

type hotActivationStatus string

const (
	hotPreparing       hotActivationStatus = "Preparing"
	hotPendingApproval hotActivationStatus = "PendingApproval"
	hotApproved        hotActivationStatus = "Approved"
	hotActivating      hotActivationStatus = "Activating"
	hotFinalizing      hotActivationStatus = "FinalizingCredentials"
	hotAborting        hotActivationStatus = "Aborting"
	hotReady           hotActivationStatus = "Ready"
	hotAborted         hotActivationStatus = "Aborted"
	hotFailed          hotActivationStatus = "Failed"
)

type hotActivationRecord struct {
	Target        pluginconfiguration.ControllerTarget `json:"target"`
	Prepare       configurationv1.PrepareRequest       `json:"prepare"`
	RequestDigest string                               `json:"requestDigest"`
	Status        hotActivationStatus                  `json:"status"`
	SubmittedBy   string                               `json:"submittedBy"`
	ApprovedBy    string                               `json:"approvedBy,omitempty"`
	CreatedAt     string                               `json:"createdAt"`
	UpdatedAt     string                               `json:"updatedAt"`
}

func (record hotActivationRecord) validate(candidate pluginconfiguration.Candidate, tenant string) error {
	if strings.TrimSpace(tenant) == "" || candidate.ApplyPath != pluginconfiguration.ApplyHotService ||
		record.Target.Protocol != configurationv1.Protocol || record.Target.ExtensionPoint != configurationv1.ExtensionPoint ||
		strings.TrimSpace(record.Target.Capability) == "" || strings.TrimSpace(record.Target.LogicalService) == "" ||
		record.Prepare.CandidateID != candidate.ID || record.Prepare.ConfigurationID != candidate.ConfigurationID ||
		record.Prepare.CatalogDigest != candidate.CatalogDigest || record.Prepare.SchemaDigest != candidate.SchemaDigest ||
		record.Prepare.ArtifactSHA256 != candidate.ArtifactSHA256 || !sameJSON(record.Prepare.Values, candidate.Values) ||
		strings.TrimSpace(record.SubmittedBy) == "" {
		return errors.New("hot activation 身份或候选绑定无效")
	}
	expectedCandidateStatus, expectedExternalStatus, ok := hotCandidateProjection(record.Status)
	if !ok || candidate.Status != expectedCandidateStatus || candidate.ExternalStatus != expectedExternalStatus {
		return errors.New("hot activation 与公开候选状态不一致")
	}
	digest, err := configurationv1.DigestPrepareRequest(record.Prepare)
	if err != nil || digest != record.RequestDigest {
		return errors.New("hot activation 请求摘要无效")
	}
	createdAt, createdErr := time.Parse(time.RFC3339Nano, record.CreatedAt)
	updatedAt, updatedErr := time.Parse(time.RFC3339Nano, record.UpdatedAt)
	if createdErr != nil || updatedErr != nil || updatedAt.Before(createdAt) {
		return errors.New("hot activation 时间无效")
	}
	switch record.Status {
	case hotPreparing, hotPendingApproval:
		if record.ApprovedBy != "" {
			return errors.New("未审批 hot activation 携带 approver")
		}
	case hotApproved, hotActivating, hotFinalizing, hotAborting, hotReady:
		if strings.TrimSpace(record.ApprovedBy) == "" || record.ApprovedBy == record.SubmittedBy {
			return errors.New("hot activation 未满足异人审批")
		}
	case hotAborted, hotFailed:
		if record.ApprovedBy != "" && record.ApprovedBy == record.SubmittedBy {
			return errors.New("终止 hot activation 审批身份无效")
		}
	default:
		return errors.New("hot activation 状态无效")
	}
	return nil
}

func hotCandidateProjection(status hotActivationStatus) (pluginconfiguration.CandidateStatus, string, bool) {
	switch status {
	case hotPreparing:
		return pluginconfiguration.CandidatePublishing, string(hotPreparing), true
	case hotPendingApproval:
		return pluginconfiguration.CandidatePublishing, string(configurationv1.StatusPrepared), true
	case hotApproved:
		return pluginconfiguration.CandidatePublishing, string(hotApproved), true
	case hotActivating:
		return pluginconfiguration.CandidateActivating, string(hotActivating), true
	case hotFinalizing:
		return pluginconfiguration.CandidateActivating, string(hotFinalizing), true
	case hotAborting:
		return pluginconfiguration.CandidateRollingBack, string(hotAborting), true
	case hotReady:
		return pluginconfiguration.CandidateReady, string(configurationv1.StatusCommitted), true
	case hotAborted:
		return pluginconfiguration.CandidateRolledBack, string(configurationv1.StatusAborted), true
	case hotFailed:
		return pluginconfiguration.CandidateFailed, string(hotFailed), true
	default:
		return "", "", false
	}
}

func sameJSON(left, right []byte) bool {
	var a, b any
	return json.Unmarshal(left, &a) == nil && json.Unmarshal(right, &b) == nil && reflect.DeepEqual(a, b)
}

func cloneHotActivation(record hotActivationRecord) hotActivationRecord {
	raw, _ := json.Marshal(record)
	var clone hotActivationRecord
	_ = json.Unmarshal(raw, &clone)
	return clone
}
