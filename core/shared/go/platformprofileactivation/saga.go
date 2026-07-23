package platformprofileactivation

import (
	"encoding/json"
	"errors"
	"strings"

	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfig"
)

const (
	DeploymentCapability       = "platform.deployment"
	CreateActivationOperation  = "createProfileConfigurationActivation"
	GetActivationOperation     = "getProfileConfigurationActivation"
	ApproveActivationOperation = "approveProfileConfigurationActivation"
	PublishActivationOperation = "publishProfileConfigurationActivation"
	AbortActivationOperation   = "abortProfileConfigurationActivation"
)

type ActivationStatus string

const (
	ActivationPreparing       ActivationStatus = "Preparing"
	ActivationPendingApproval ActivationStatus = "PendingApproval"
	ActivationApproved        ActivationStatus = "Approved"
	ActivationCatalogActive   ActivationStatus = "CatalogActivated"
	ActivationPublishing      ActivationStatus = "Publishing"
	ActivationReady           ActivationStatus = "Ready"
	ActivationRollingBack     ActivationStatus = "RollingBack"
	ActivationRolledBack      ActivationStatus = "RolledBack"
	ActivationAborted         ActivationStatus = "Aborted"
	ActivationFailed          ActivationStatus = "Failed"
)

// CreateActivationRequest is the plugin-settings-to-deployment-manager
// boundary. Target service, Application Composition, Profile and Catalog are
// deliberately absent and are re-derived by trusted control-plane code.
type CreateActivationRequest struct {
	CandidateID         string                                       `json:"candidateId"`
	ConfigurationID     string                                       `json:"configurationId"`
	ConfigCatalogDigest string                                       `json:"configCatalogDigest"`
	SchemaDigest        string                                       `json:"schemaDigest"`
	ArtifactSHA256      string                                       `json:"artifactSha256"`
	Values              json.RawMessage                              `json:"values"`
	Credentials         map[string]pluginconfig.ManagedCredentialRef `json:"credentials,omitempty"`
}

type ActivationLookup struct {
	CandidateID string `json:"candidateId"`
}

type Activation struct {
	CandidateID                string           `json:"candidateId"`
	ConfigurationID            string           `json:"configurationId"`
	Deployment                 string           `json:"deployment"`
	DeploymentRevision         uint64           `json:"deploymentRevision"`
	PreviousServiceRevision    uint64           `json:"previousServiceRevision"`
	RollbackDeploymentRevision uint64           `json:"rollbackDeploymentRevision,omitempty"`
	Status                     ActivationStatus `json:"status"`
	RequestedBy                string           `json:"requestedBy"`
	ApprovedBy                 string           `json:"approvedBy,omitempty"`
	ErrorCode                  string           `json:"errorCode,omitempty"`
	ErrorMessage               string           `json:"errorMessage,omitempty"`
}

func (request CreateActivationRequest) Validate() error {
	if !validHexID(request.CandidateID, "pcfg_", 32) || !validHexID(request.ConfigurationID, "cfg_", 24) ||
		!validHexID(request.ConfigCatalogDigest, "", 64) || !validHexID(request.SchemaDigest, "", 64) ||
		!validHexID(request.ArtifactSHA256, "", 64) {
		return errors.New("Platform Profile 配置激活身份无效")
	}
	var values map[string]any
	if len(request.Values) == 0 || json.Unmarshal(request.Values, &values) != nil || values == nil {
		return errors.New("Platform Profile 配置激活 values 必须是 JSON 对象")
	}
	if _, err := normalizeCredentials(request.Credentials); err != nil {
		return err
	}
	return nil
}

func (request ActivationLookup) Validate() error {
	if !validHexID(request.CandidateID, "pcfg_", 32) {
		return errors.New("Platform Profile 配置激活候选身份无效")
	}
	return nil
}

func (activation Activation) Validate() error {
	if !validHexID(activation.CandidateID, "pcfg_", 32) || !validHexID(activation.ConfigurationID, "cfg_", 24) ||
		strings.TrimSpace(activation.Deployment) == "" || activation.DeploymentRevision == 0 || activation.PreviousServiceRevision == 0 || strings.TrimSpace(activation.RequestedBy) == "" {
		return errors.New("Platform Profile 配置激活响应身份无效")
	}
	switch activation.Status {
	case ActivationPreparing, ActivationPendingApproval:
		if activation.ApprovedBy != "" || activation.RollbackDeploymentRevision != 0 {
			return errors.New("未审批 Platform Profile 激活携带无效审批或回滚状态")
		}
	case ActivationAborted, ActivationFailed:
		if activation.RollbackDeploymentRevision != 0 || (activation.ApprovedBy != "" && activation.ApprovedBy == activation.RequestedBy) {
			return errors.New("终止的 Platform Profile 激活审批或回滚状态无效")
		}
	case ActivationApproved, ActivationCatalogActive, ActivationPublishing, ActivationReady:
		if strings.TrimSpace(activation.ApprovedBy) == "" || activation.ApprovedBy == activation.RequestedBy || activation.RollbackDeploymentRevision != 0 {
			return errors.New("已审批 Platform Profile 激活状态无效")
		}
	case ActivationRollingBack:
		if strings.TrimSpace(activation.ApprovedBy) == "" || activation.ApprovedBy == activation.RequestedBy {
			return errors.New("回滚中的 Platform Profile 激活审批状态无效")
		}
	case ActivationRolledBack:
		if strings.TrimSpace(activation.ApprovedBy) == "" || activation.ApprovedBy == activation.RequestedBy || activation.RollbackDeploymentRevision <= activation.DeploymentRevision {
			return errors.New("已回滚 Platform Profile 激活缺少单调回滚修订")
		}
	default:
		return errors.New("Platform Profile 配置激活响应状态无效")
	}
	return nil
}
