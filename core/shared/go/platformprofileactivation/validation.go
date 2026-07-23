package platformprofileactivation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	commonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/common/v1"
	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfig"
)

func NormalizePrepareRequest(request PrepareRequest) (PrepareRequest, error) {
	if !validHexID(request.CandidateID, "pcfg_", 32) || !validHexID(request.ConfigurationID, "cfg_", 24) ||
		!validHexID(request.ConfigCatalogDigest, "", 64) || !validHexID(request.SchemaDigest, "", 64) ||
		!validHexID(request.ArtifactSHA256, "", 64) || request.DeploymentRevision == 0 {
		return PrepareRequest{}, errors.New("Platform Profile 配置候选身份无效")
	}
	composition, err := backendcompositionv1.ValidateApplicationComposition(request.Composition)
	if err != nil || strings.TrimSpace(composition.Metadata.Tenant) == "" || strings.TrimSpace(composition.Metadata.Name) == "" || composition.ID != composition.Metadata.Name {
		return PrepareRequest{}, errors.New("Platform Profile 配置候选 Application Composition 无效")
	}
	var values map[string]any
	if len(request.Values) == 0 || json.Unmarshal(request.Values, &values) != nil || values == nil {
		return PrepareRequest{}, errors.New("Platform Profile 配置 values 必须是 JSON 对象")
	}
	request.Values, err = json.Marshal(values)
	if err != nil {
		return PrepareRequest{}, err
	}
	request.Composition = composition
	request.Credentials, err = normalizeCredentials(request.Credentials)
	if err != nil {
		return PrepareRequest{}, err
	}
	return request, nil
}

func DigestPrepareRequest(request PrepareRequest) (string, error) {
	normalized, err := NormalizePrepareRequest(request)
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(normalized)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

func (request CandidateRequest) Validate() error {
	if !validHexID(request.CandidateID, "pcfg_", 32) || !validHexID(request.RequestDigest, "", 64) {
		return errors.New("Platform Profile 候选恢复身份无效")
	}
	return nil
}

func (request PublishRequest) Normalize() (PublishRequest, error) {
	normalized, err := NormalizePrepareRequest(request.Prepare)
	if err != nil {
		return PublishRequest{}, err
	}
	digest, err := DigestPrepareRequest(normalized)
	if err != nil || digest != request.RequestDigest || !validHexID(request.ExpectedDigest, "", 64) {
		return PublishRequest{}, errors.New("Platform Profile 候选发布摘要无效")
	}
	request.Prepare = normalized
	return request, nil
}

func (candidate Candidate) Validate() error {
	if !validHexID(candidate.CandidateID, "pcfg_", 32) || !validHexID(candidate.RequestDigest, "", 64) ||
		!validHexID(candidate.ConfigurationID, "cfg_", 24) || strings.TrimSpace(candidate.Deployment) == "" ||
		!validRef(candidate.PreviousProfile) || !validRef(candidate.NextProfile) ||
		!validHexID(candidate.ExpectedCatalogDigest, "", 64) || !validHexID(candidate.NextCatalogDigest, "", 64) {
		return errors.New("Platform Profile 候选响应身份无效")
	}
	switch candidate.Status {
	case StatusPrepared, StatusActivated, StatusFinalized, StatusAborted:
		if candidate.RollbackCatalogDigest != "" {
			return errors.New("未回滚 Platform Profile 候选不得携带回滚摘要")
		}
	case StatusRolledBack:
		if !validHexID(candidate.RollbackCatalogDigest, "", 64) {
			return errors.New("已回滚 Platform Profile 候选缺少回滚摘要")
		}
	default:
		return errors.New("Platform Profile 候选响应状态无效")
	}
	return nil
}

func normalizeCredentials(values map[string]pluginconfig.ManagedCredentialRef) (map[string]pluginconfig.ManagedCredentialRef, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make(map[string]pluginconfig.ManagedCredentialRef, len(values))
	for fieldID, ref := range values {
		if strings.TrimSpace(fieldID) == "" || ref.Scope != "tenant" || commonv1.ValidateManagedCredentialRef(ref) != nil {
			return nil, errors.New("Platform Profile 配置候选包含无效凭证引用")
		}
		out[fieldID] = ref
	}
	return out, nil
}

func validRef(ref compositioncommonv1.Ref) bool {
	return strings.TrimSpace(ref.ID) != "" && ref.Revision > 0 && validHexID(ref.Digest, "", 64)
}

func validHexID(value, prefix string, length int) bool {
	if !strings.HasPrefix(value, prefix) || len(value) != len(prefix)+length {
		return false
	}
	for _, character := range value[len(prefix):] {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}
