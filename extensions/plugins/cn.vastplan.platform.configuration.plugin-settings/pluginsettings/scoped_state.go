package pluginsettings

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	configurationscopedv1 "cdsoft.com.cn/VastPlan/contracts/schemas/configurationscoped/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfiguration"
)

type scopedActiveReference struct {
	Revision uint64 `json:"revision"`
	Digest   string `json:"digest"`
}

type scopedActiveRecord struct {
	ConfigurationID string          `json:"configurationId"`
	PluginID        string          `json:"pluginId"`
	Scope           string          `json:"scope"`
	SubjectID       string          `json:"subjectId,omitempty"`
	Revision        uint64          `json:"revision"`
	Digest          string          `json:"digest"`
	SchemaDigest    string          `json:"schemaDigest"`
	ArtifactSHA256  string          `json:"artifactSha256"`
	Values          json.RawMessage `json:"values"`
	CandidateID     string          `json:"candidateId"`
	UpdatedAt       string          `json:"updatedAt"`
}

type scopedActivationStatus string

const (
	scopedPendingApproval scopedActivationStatus = "PendingApproval"
	scopedApproved        scopedActivationStatus = "Approved"
)

type scopedActivationRecord struct {
	Base        scopedActiveReference  `json:"base"`
	Status      scopedActivationStatus `json:"status"`
	SubmittedBy string                 `json:"submittedBy"`
	ApprovedBy  string                 `json:"approvedBy,omitempty"`
	CreatedAt   string                 `json:"createdAt"`
	UpdatedAt   string                 `json:"updatedAt"`
}

func scopedRecordKey(configurationID, subjectID string) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf("%d:%s\n%d:%s\n", len(configurationID), configurationID, len(subjectID), subjectID)))
	return "scfg_" + hex.EncodeToString(digest[:16])
}

func scopedSubject(definition pluginconfiguration.Definition, requested string) (string, error) {
	requested = strings.TrimSpace(requested)
	switch definition.Scope {
	case string(configurationscopedv1.ScopeTenant):
		if requested != "" {
			return "", errors.New("tenant scoped 配置不得指定 subject")
		}
		return "", nil
	case string(configurationscopedv1.ScopeUser):
		if requested == "" || len(requested) > 256 {
			return "", errors.New("user scoped 配置必须指定有效 subject")
		}
		return requested, nil
	default:
		return "", errors.New("配置不是 hot-scoped")
	}
}

func (r scopedActiveRecord) reference() scopedActiveReference {
	return scopedActiveReference{Revision: r.Revision, Digest: r.Digest}
}

func (r scopedActiveRecord) validate(key string) error {
	if key != scopedRecordKey(r.ConfigurationID, r.SubjectID) || r.ConfigurationID == "" || r.PluginID == "" ||
		(r.Scope != string(configurationscopedv1.ScopeTenant) && r.Scope != string(configurationscopedv1.ScopeUser)) ||
		(r.Scope == string(configurationscopedv1.ScopeTenant) && r.SubjectID != "") ||
		(r.Scope == string(configurationscopedv1.ScopeUser) && strings.TrimSpace(r.SubjectID) == "") ||
		r.Revision == 0 || len(r.Digest) != 64 || len(r.SchemaDigest) != 64 || len(r.ArtifactSHA256) != 64 ||
		r.CandidateID == "" || r.UpdatedAt == "" || !json.Valid(r.Values) {
		return errors.New("scoped active 身份无效")
	}
	digest, err := configurationscopedv1.DigestValues(r.Values)
	if err != nil || digest != r.Digest {
		return errors.New("scoped active 值摘要无效")
	}
	return nil
}

func (r scopedActivationRecord) validate(candidate pluginconfiguration.Candidate) error {
	if r.Base.Digest == "" || r.SubmittedBy == "" || r.CreatedAt == "" || r.UpdatedAt == "" {
		return errors.New("scoped activation 身份无效")
	}
	switch r.Status {
	case scopedPendingApproval:
		if r.ApprovedBy != "" || candidate.ExternalStatus != string(scopedPendingApproval) {
			return errors.New("scoped pending approval 状态无效")
		}
	case scopedApproved:
		if r.ApprovedBy == "" || r.ApprovedBy == r.SubmittedBy || candidate.ExternalStatus != string(scopedApproved) {
			return errors.New("scoped approved 状态无效")
		}
	default:
		return errors.New("scoped activation 状态无效")
	}
	return nil
}

func (s *Service) scopedChangeChannelLocked(tenant, key string) <-chan struct{} {
	watchKey := tenant + "\x00" + key
	channel := s.scopedChanged[watchKey]
	if channel == nil {
		channel = make(chan struct{})
		s.scopedChanged[watchKey] = channel
	}
	return channel
}

func (s *Service) notifyScopedChangedLocked(tenant, key string) {
	watchKey := tenant + "\x00" + key
	if channel := s.scopedChanged[watchKey]; channel != nil {
		close(channel)
	}
	s.scopedChanged[watchKey] = make(chan struct{})
}
