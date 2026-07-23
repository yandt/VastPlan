package artifactassessment

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

type trustedKey struct {
	publicKey ed25519.PublicKey
	config    ProviderKey
}

type Verifier struct {
	policy TrustPolicy
	keys   map[string]trustedKey
}

func NewVerifier(policy TrustPolicy) (*Verifier, error) {
	policy = normalizePolicy(policy)
	if len(policy.RequiredChannels) > 16 || len(policy.Keys) > 128 || len(policy.Requirements) > 256 || policy.MaxRecordTTLHours < 1 || policy.MaxRecordTTLHours > 8_760 {
		return nil, errors.New("安全评估策略数量或有效期超限")
	}
	if err := unique(policy.RequiredChannels, "requiredChannels", true); err != nil {
		return nil, err
	}
	keys := make(map[string]trustedKey, len(policy.Keys))
	for _, item := range policy.Keys {
		if err := validateIdentityFields(item.ProviderID, item.KeyID); err != nil {
			return nil, err
		}
		if item.NotBefore != nil && item.NotBefore.Location() != time.UTC || item.NotAfter != nil && item.NotAfter.Location() != time.UTC || item.NotBefore != nil && item.NotAfter != nil && !item.NotBefore.Before(*item.NotAfter) {
			return nil, errors.New("安全评估 Provider key 时间窗口无效")
		}
		raw, err := base64.StdEncoding.DecodeString(item.PublicKey)
		if err != nil || len(raw) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("安全评估 Provider key %s/%s 无效", item.ProviderID, item.KeyID)
		}
		id := keyID(item.ProviderID, item.KeyID)
		if _, exists := keys[id]; exists {
			return nil, errors.New("安全评估 Provider key 重复")
		}
		keys[id] = trustedKey{publicKey: append(ed25519.PublicKey(nil), raw...), config: item}
	}
	selectors := map[string]struct{}{}
	for _, requirement := range policy.Requirements {
		if err := validateRequirement(requirement); err != nil {
			return nil, err
		}
		selector := requirement.Channel + "\x00" + requirement.Publisher + "\x00" + requirement.PluginPrefix
		if _, exists := selectors[selector]; exists {
			return nil, errors.New("安全评估 requirement selector 重复")
		}
		selectors[selector] = struct{}{}
		for _, provider := range requirement.ProviderIDs {
			found := false
			for _, key := range policy.Keys {
				found = found || key.ProviderID == provider
			}
			if !found {
				return nil, fmt.Errorf("安全评估 requirement %s 引用了没有 key 的 Provider %s", requirement.ID, provider)
			}
		}
	}
	for _, channel := range policy.RequiredChannels {
		found := false
		for _, requirement := range policy.Requirements {
			found = found || requirement.Channel == channel
		}
		if !found {
			return nil, fmt.Errorf("强制安全评估 channel %s 没有 requirement", channel)
		}
	}
	return &Verifier{policy: policy, keys: keys}, nil
}

func (v *Verifier) Required(channel string) bool {
	return v != nil && slices.Contains(v.policy.RequiredChannels, channel)
}

func (v *Verifier) VerifyAdmission(identity ArtifactIdentity, raw []byte, now time.Time) (*AdmissionRecord, string, error) {
	if v == nil {
		if len(raw) != 0 {
			return nil, "", errors.New("安全评估验证器未配置")
		}
		return nil, "", nil
	}
	if len(raw) == 0 && !v.Required(identity.Channel) {
		return nil, "", nil
	}
	requirement, err := v.selectRequirement(identity)
	if err != nil {
		return nil, "", err
	}
	record, recordDigest, err := InspectAdmission(raw)
	if err != nil {
		return nil, "", err
	}
	if err := v.verifyEvaluation(identity, requirement, record.ProviderID, record.KeyID, record.PolicyID, record.Evaluation, now, false); err != nil {
		return nil, "", err
	}
	key := v.keys[keyID(record.ProviderID, record.KeyID)]
	if err := verifyAdmissionSignature(record, key.publicKey); err != nil {
		return nil, "", err
	}
	copy := record
	return &copy, recordDigest, nil
}

// VerifyStatus validates signature, identity and append-only position. A failed
// decision is returned as verified evidence; EnforceDecision decides whether an
// install or activation may proceed.
func (v *Verifier) VerifyStatus(identity ArtifactIdentity, admissionRaw, previousRaw, statusRaw []byte, now time.Time) (*StatusRecord, string, error) {
	inspectedAdmission, _, inspectErr := InspectAdmission(admissionRaw)
	if inspectErr != nil {
		return nil, "", errors.New("安全复扫状态缺少有效准入记录")
	}
	// Admission is the immutable chain root. A fresh rescan may legitimately
	// extend operational trust after that admission's TTL elapsed, so verify the
	// root at its evaluation time while still honoring key revocation. The new
	// status itself is always checked against the current trusted clock.
	admission, admissionDigest, err := v.VerifyAdmission(identity, admissionRaw, inspectedAdmission.Evaluation.EvaluatedAt)
	if err != nil || admission == nil {
		return nil, "", errors.New("安全复扫状态缺少有效准入记录")
	}
	status, statusDigest, err := InspectStatus(statusRaw)
	if err != nil {
		return nil, "", err
	}
	if status.AdmissionSHA256 != admissionDigest || status.Evaluation.SubjectSHA256 != admission.Evaluation.SubjectSHA256 || status.Evaluation.SBOMSHA256 != admission.Evaluation.SBOMSHA256 {
		return nil, "", errors.New("安全复扫状态未绑定当前准入记录")
	}
	if status.Sequence == 1 {
		if len(previousRaw) != 0 || status.PreviousSHA256 != admissionDigest {
			return nil, "", errors.New("首条安全复扫状态链位置无效")
		}
	} else {
		previous, previousDigest, inspectErr := InspectStatus(previousRaw)
		if inspectErr != nil || previousDigest != status.PreviousSHA256 || previous.Sequence+1 != status.Sequence || previous.AdmissionSHA256 != admissionDigest {
			return nil, "", errors.New("安全复扫状态序号或前序摘要无效")
		}
	}
	requirement, err := v.selectRequirement(identity)
	if err != nil {
		return nil, "", err
	}
	if err := v.verifyEvaluation(identity, requirement, status.ProviderID, status.KeyID, status.PolicyID, status.Evaluation, now, true); err != nil {
		return nil, "", err
	}
	key := v.keys[keyID(status.ProviderID, status.KeyID)]
	if err := verifyStatusSignature(status, key.publicKey); err != nil {
		return nil, "", err
	}
	return &status, statusDigest, nil
}

func EnforceDecision(value Evaluation) error {
	if value.Decision != DecisionPass {
		return errors.New("安全评估未通过，禁止安装或激活制品")
	}
	return nil
}

func (v *Verifier) verifyEvaluation(identity ArtifactIdentity, requirement Requirement, providerID, keyIDValue, policyID string, evaluation Evaluation, now time.Time, allowFailedDecision bool) error {
	if !validSHA256(identity.SHA256) || !validSHA256(identity.SBOMSHA256) || evaluation.SubjectSHA256 != identity.SHA256 || evaluation.SBOMSHA256 != identity.SBOMSHA256 || policyID != requirement.ID {
		return errors.New("安全评估未绑定当前制品、SBOM 或选中策略")
	}
	key, exists := v.keys[keyID(providerID, keyIDValue)]
	if !exists || !slices.Contains(requirement.ProviderIDs, providerID) || !slices.Contains(requirement.ScannerIDs, evaluation.Scanner.ID) {
		return errors.New("安全评估 Provider、key 或 scanner 不受策略信任")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if now.Location() != time.UTC || evaluation.EvaluatedAt.After(now.Add(MaxClockSkew)) || !evaluation.ExpiresAt.After(now) || evaluation.ExpiresAt.Sub(evaluation.EvaluatedAt) > time.Duration(v.policy.MaxRecordTTLHours)*time.Hour {
		return errors.New("安全评估已过期、来自未来或有效期过长")
	}
	if key.config.Revoked || key.config.NotBefore != nil && now.Before(*key.config.NotBefore) || key.config.NotAfter != nil && now.After(*key.config.NotAfter) || key.config.NotBefore != nil && evaluation.EvaluatedAt.Before(*key.config.NotBefore) || key.config.NotAfter != nil && evaluation.EvaluatedAt.After(*key.config.NotAfter) {
		return errors.New("安全评估 Provider key 已撤销、未生效或已过期")
	}
	if requirement.RequireReportDigests && (!validSHA256(evaluation.Vulnerabilities.ReportSHA256) || !validSHA256(evaluation.Licenses.ReportSHA256)) {
		return errors.New("安全评估策略要求绑定漏洞与许可证报告摘要")
	}
	if evaluation.Decision == DecisionFail && allowFailedDecision {
		return nil
	}
	if evaluation.Decision != DecisionPass {
		return errors.New("安全准入记录未通过选中策略")
	}
	if err := enforceMaximum(requirement.Maximum, evaluation); err != nil {
		return err
	}
	return nil
}

func enforceMaximum(max MaximumFindings, evaluation Evaluation) error {
	checks := []struct {
		limit  *uint64
		actual uint64
		name   string
	}{
		{max.Critical, evaluation.Vulnerabilities.Critical, "critical 漏洞"},
		{max.High, evaluation.Vulnerabilities.High, "high 漏洞"},
		{max.Medium, evaluation.Vulnerabilities.Medium, "medium 漏洞"},
		{max.Low, evaluation.Vulnerabilities.Low, "low 漏洞"},
		{max.UnknownVulnerability, evaluation.Vulnerabilities.Unknown, "未知漏洞"},
		{max.DeniedLicense, evaluation.Licenses.Denied, "拒绝许可证"},
		{max.UnknownLicense, evaluation.Licenses.Unknown, "未知许可证"},
	}
	for _, check := range checks {
		if check.limit != nil && check.actual > *check.limit {
			return fmt.Errorf("安全评估 %s 数量 %d 超过策略上限 %d", check.name, check.actual, *check.limit)
		}
	}
	return nil
}

func normalizePolicy(policy TrustPolicy) TrustPolicy {
	if policy.RequiredChannels == nil {
		policy.RequiredChannels = []string{"stable"}
	}
	if policy.MaxRecordTTLHours == 0 {
		policy.MaxRecordTTLHours = 168
	}
	return policy
}

func validateRequirement(value Requirement) error {
	if err := validateIdentityFields(value.ID, value.Channel); err != nil {
		return err
	}
	if strings.TrimSpace(value.Publisher) != value.Publisher || strings.TrimSpace(value.PluginPrefix) != value.PluginPrefix || len(value.Publisher) > 160 || len(value.PluginPrefix) > 160 {
		return errors.New("安全评估 selector 未规范化或超限")
	}
	if err := unique(value.ProviderIDs, "providerIds", false); err != nil {
		return err
	}
	return unique(value.ScannerIDs, "scannerIds", false)
}

func unique(values []string, name string, allowEmpty bool) error {
	if !allowEmpty && len(values) == 0 {
		return fmt.Errorf("%s 不能为空", name)
	}
	for index, value := range values {
		if value == "" || strings.TrimSpace(value) != value || len(value) > 256 || slices.Contains(values[:index], value) {
			return fmt.Errorf("%s 必须规范、非空且不重复", name)
		}
	}
	return nil
}

func (v *Verifier) selectRequirement(identity ArtifactIdentity) (Requirement, error) {
	best, score, found := Requirement{}, -1, false
	for _, candidate := range v.policy.Requirements {
		if candidate.Channel != identity.Channel || candidate.Publisher != "" && candidate.Publisher != identity.Publisher || candidate.PluginPrefix != "" && !matchesPrefix(identity.PluginID, candidate.PluginPrefix) {
			continue
		}
		candidateScore := len(candidate.PluginPrefix) * 2
		if candidate.Publisher != "" {
			candidateScore += 10_000
		}
		if candidateScore == score {
			return Requirement{}, errors.New("安全评估 requirement 选择歧义")
		}
		if candidateScore > score {
			best, score, found = candidate, candidateScore, true
		}
	}
	if !found {
		return Requirement{}, errors.New("当前制品没有匹配的安全评估 requirement")
	}
	return best, nil
}

func matchesPrefix(pluginID, prefix string) bool {
	if strings.HasSuffix(prefix, ".") {
		return strings.HasPrefix(pluginID, prefix)
	}
	return pluginID == prefix || strings.HasPrefix(pluginID, prefix+".")
}

func keyID(providerID, keyID string) string { return providerID + "\x00" + keyID }
