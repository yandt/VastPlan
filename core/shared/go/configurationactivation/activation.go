// Package configurationactivation defines the narrow, non-secret contract
// between plugin-settings and deployment-manager for application-owned
// restart configuration. It deliberately carries credential references, never
// credential material, authority tokens or custodian stage identifiers.
package configurationactivation

import (
	"encoding/json"
	"errors"
	"strings"

	commonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/common/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfig"
)

const (
	DeploymentCapability = "platform.deployment"
	CreateOperation      = "createConfigurationActivation"
	GetOperation         = "getConfigurationActivation"
	PublishOperation     = "publishConfigurationActivation"
)

type Status string

const (
	StatusPendingApproval Status = "PendingApproval"
	StatusApproved        Status = "Approved"
	StatusPublishing      Status = "Publishing"
	StatusReady           Status = "Ready"
	StatusFailed          Status = "Failed"
	StatusRolledBack      Status = "RolledBack"
)

type CreateRequest struct {
	CandidateID     string                                       `json:"candidateId"`
	ConfigurationID string                                       `json:"configurationId"`
	CatalogDigest   string                                       `json:"catalogDigest"`
	SchemaDigest    string                                       `json:"schemaDigest"`
	ArtifactSHA256  string                                       `json:"artifactSha256"`
	Values          json.RawMessage                              `json:"values"`
	Credentials     map[string]pluginconfig.ManagedCredentialRef `json:"credentials,omitempty"`
}

type LookupRequest struct {
	CandidateID string `json:"candidateId"`
}

type Activation struct {
	CandidateID             string `json:"candidateId"`
	ConfigurationID         string `json:"configurationId"`
	Deployment              string `json:"deployment"`
	ServiceRevision         uint64 `json:"serviceRevision"`
	PreviousServiceRevision uint64 `json:"previousServiceRevision,omitempty"`
	RollbackServiceRevision uint64 `json:"rollbackServiceRevision,omitempty"`
	Status                  Status `json:"status"`
	ErrorCode               string `json:"errorCode,omitempty"`
	ErrorMessage            string `json:"errorMessage,omitempty"`
}

func (r CreateRequest) Validate() error {
	if !validID(r.CandidateID, "pcfg_", 32) || !validID(r.ConfigurationID, "cfg_", 24) ||
		!validID(r.CatalogDigest, "", 64) || !validID(r.SchemaDigest, "", 64) || !validID(r.ArtifactSHA256, "", 64) ||
		len(r.Values) == 0 || !json.Valid(r.Values) {
		return errors.New("配置激活请求身份无效")
	}
	for fieldID, ref := range r.Credentials {
		if strings.TrimSpace(fieldID) == "" || ref.Scope != "tenant" || commonv1.ValidateManagedCredentialRef(ref) != nil {
			return errors.New("配置激活请求包含无效凭证引用")
		}
	}
	return nil
}

func (r LookupRequest) Validate() error {
	if !validID(r.CandidateID, "pcfg_", 32) {
		return errors.New("配置激活候选身份无效")
	}
	return nil
}

func (a Activation) Validate() error {
	if !validID(a.CandidateID, "pcfg_", 32) || !validID(a.ConfigurationID, "cfg_", 24) ||
		strings.TrimSpace(a.Deployment) == "" || a.ServiceRevision == 0 {
		return errors.New("配置激活响应身份无效")
	}
	switch a.Status {
	case StatusPendingApproval, StatusApproved, StatusPublishing, StatusReady, StatusFailed, StatusRolledBack:
		return nil
	default:
		return errors.New("配置激活响应状态无效")
	}
}

func validID(value, prefix string, hexLength int) bool {
	if !strings.HasPrefix(value, prefix) || len(value) != len(prefix)+hexLength {
		return false
	}
	for _, character := range value[len(prefix):] {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}
