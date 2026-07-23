package artifactprovenance

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

const maximumClockSkew = 5 * time.Minute

type trustedKey struct {
	publicKey ed25519.PublicKey
	config    VerifierKey
}

type Verifier struct {
	policy TrustPolicy
	keys   map[string]trustedKey
}

func NewVerifier(policy TrustPolicy) (*Verifier, error) {
	policy = normalizedPolicy(policy)
	if len(policy.RequiredChannels) > 16 || len(policy.Keys) > 128 || len(policy.Requirements) > 256 || policy.MaxRecordTTLHours < 1 || policy.MaxRecordTTLHours > 87_600 {
		return nil, errors.New("来源证明信任策略数量或有效期超限")
	}
	if err := validateUniqueStrings(policy.RequiredChannels, "requiredChannels", true); err != nil {
		return nil, err
	}
	keys := make(map[string]trustedKey, len(policy.Keys))
	for _, key := range policy.Keys {
		if strings.TrimSpace(key.ProviderID) != key.ProviderID || strings.TrimSpace(key.KeyID) != key.KeyID || key.ProviderID == "" || key.KeyID == "" || len(key.ProviderID) > 160 || len(key.KeyID) > 160 {
			return nil, errors.New("Verifier key providerId/keyId 无效")
		}
		if key.NotBefore != nil && key.NotBefore.Location() != time.UTC || key.NotAfter != nil && key.NotAfter.Location() != time.UTC || key.NotBefore != nil && key.NotAfter != nil && !key.NotBefore.Before(*key.NotAfter) {
			return nil, errors.New("Verifier key 时间窗口无效")
		}
		decoded, err := base64.StdEncoding.DecodeString(key.PublicKey)
		if err != nil || len(decoded) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("Verifier key %s/%s 不是 Ed25519 公钥", key.ProviderID, key.KeyID)
		}
		identity := keyIdentity(key.ProviderID, key.KeyID)
		if _, exists := keys[identity]; exists {
			return nil, fmt.Errorf("Verifier key 重复: %s/%s", key.ProviderID, key.KeyID)
		}
		keys[identity] = trustedKey{publicKey: append(ed25519.PublicKey(nil), decoded...), config: key}
	}
	selectors := map[string]struct{}{}
	for index := range policy.Requirements {
		requirement := &policy.Requirements[index]
		if err := validateRequirement(*requirement); err != nil {
			return nil, fmt.Errorf("来源证明 requirement %d: %w", index, err)
		}
		selector := requirement.Channel + "\x00" + requirement.Publisher + "\x00" + requirement.PluginPrefix
		if _, exists := selectors[selector]; exists {
			return nil, errors.New("来源证明 requirement selector 重复")
		}
		selectors[selector] = struct{}{}
		for _, providerID := range requirement.ProviderIDs {
			found := false
			for _, key := range policy.Keys {
				found = found || key.ProviderID == providerID
			}
			if !found {
				return nil, fmt.Errorf("来源证明 requirement %s 引用了没有 key 的 Provider %s", requirement.ID, providerID)
			}
		}
	}
	for _, channel := range policy.RequiredChannels {
		found := false
		for _, requirement := range policy.Requirements {
			found = found || requirement.Channel == channel
		}
		if !found {
			return nil, fmt.Errorf("强制来源证明 channel %s 没有 requirement", channel)
		}
	}
	return &Verifier{policy: policy, keys: keys}, nil
}

func (v *Verifier) Required(channel string) bool {
	return v != nil && slices.Contains(v.policy.RequiredChannels, channel)
}

func (v *Verifier) Verify(identity ArtifactIdentity, provenanceRaw, recordRaw []byte, now time.Time) (*VerificationRecord, error) {
	if v == nil {
		if len(provenanceRaw) != 0 || len(recordRaw) != 0 {
			return nil, errors.New("来源证明验证器未配置")
		}
		return nil, nil
	}
	required := v.Required(identity.Channel)
	if len(provenanceRaw) == 0 && len(recordRaw) == 0 && !required {
		return nil, nil
	}
	if len(provenanceRaw) == 0 || len(recordRaw) == 0 {
		return nil, errors.New("原始 Provenance 与 Verification Record 必须同时提供")
	}
	if !validSHA256(identity.SHA256) || strings.TrimSpace(identity.PluginID) == "" || strings.TrimSpace(identity.Publisher) == "" {
		return nil, errors.New("待验证制品身份无效")
	}
	requirement, err := v.selectRequirement(identity)
	if err != nil {
		return nil, err
	}
	record, err := decodeRecord(recordRaw)
	if err != nil {
		return nil, fmt.Errorf("解析 Verification Record: %w", err)
	}
	if record.SubjectSHA256 != identity.SHA256 || record.PolicyID != requirement.ID {
		return nil, errors.New("Verification Record 未绑定当前制品或选中策略")
	}
	summary, provenanceDigest, err := InspectDSSE(provenanceRaw, identity.SHA256)
	if err != nil {
		return nil, err
	}
	if record.ProvenanceSHA256 != provenanceDigest || !sameSummary(record.StatementSummary, summary) {
		return nil, errors.New("Verification Record 与原始 Provenance 摘要不一致")
	}
	key, exists := v.keys[keyIdentity(record.ProviderID, record.KeyID)]
	if !exists || !slices.Contains(requirement.ProviderIDs, record.ProviderID) {
		return nil, errors.New("Verification Record Provider/key 不受选中策略信任")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if now.Location() != time.UTC || record.VerifiedAt.After(now.Add(maximumClockSkew)) || !record.ExpiresAt.After(now) || record.ExpiresAt.Sub(record.VerifiedAt) > time.Duration(v.policy.MaxRecordTTLHours)*time.Hour {
		return nil, errors.New("Verification Record 已过期、来自未来或有效期过长")
	}
	if key.config.Revoked || key.config.NotBefore != nil && now.Before(*key.config.NotBefore) || key.config.NotAfter != nil && now.After(*key.config.NotAfter) || key.config.NotBefore != nil && record.VerifiedAt.Before(*key.config.NotBefore) || key.config.NotAfter != nil && record.VerifiedAt.After(*key.config.NotAfter) {
		return nil, errors.New("Verifier key 已撤销、未生效或已过期")
	}
	if err := verifyRecordSignature(record, key.publicKey); err != nil {
		return nil, err
	}
	if err := enforceRequirement(requirement, record); err != nil {
		return nil, err
	}
	copy := record
	copy.Sources = cloneSources(record.Sources)
	return &copy, nil
}

func normalizedPolicy(policy TrustPolicy) TrustPolicy {
	if policy.RequiredChannels == nil {
		policy.RequiredChannels = []string{"stable"}
	}
	if policy.MaxRecordTTLHours == 0 {
		policy.MaxRecordTTLHours = 8_760
	}
	return policy
}

func validateRequirement(value Requirement) error {
	if value.ID == "" || len(value.ID) > 160 || value.Channel == "" || len(value.Channel) > 64 || len(value.PluginPrefix) > 160 || len(value.Publisher) > 160 {
		return errors.New("selector 或 id 无效")
	}
	if strings.TrimSpace(value.ID) != value.ID || strings.TrimSpace(value.Channel) != value.Channel || strings.TrimSpace(value.Publisher) != value.Publisher || strings.TrimSpace(value.PluginPrefix) != value.PluginPrefix {
		return errors.New("selector 或 id 必须使用规范非空白值")
	}
	if len(value.ProviderIDs) == 0 || len(value.BuilderIDs) == 0 || len(value.BuildTypes) == 0 || len(value.SourceURIPrefixes) == 0 {
		return errors.New("provider/builder/buildType/source 允许列表不能为空")
	}
	for name, values := range map[string][]string{"providerIds": value.ProviderIDs, "builderIds": value.BuilderIDs, "buildTypes": value.BuildTypes, "sourceUriPrefixes": value.SourceURIPrefixes, "issuers": value.Issuers, "workflows": value.Workflows} {
		if len(values) > 128 {
			return fmt.Errorf("%s 超过 128", name)
		}
		if err := validateUniqueStrings(values, name, true); err != nil {
			return err
		}
	}
	return nil
}

func validateUniqueStrings(values []string, name string, allowEmptyList bool) error {
	if !allowEmptyList && len(values) == 0 {
		return fmt.Errorf("%s 不能为空", name)
	}
	for index, value := range values {
		if value == "" || strings.TrimSpace(value) != value || len(value) > 4096 || slices.Contains(values[:index], value) {
			return fmt.Errorf("%s 必须规范、非空且不重复", name)
		}
	}
	return nil
}

func (v *Verifier) selectRequirement(identity ArtifactIdentity) (Requirement, error) {
	best, bestScore, found := Requirement{}, -1, false
	for _, candidate := range v.policy.Requirements {
		if candidate.Channel != identity.Channel || candidate.Publisher != "" && candidate.Publisher != identity.Publisher || candidate.PluginPrefix != "" && !matchesPluginPrefix(identity.PluginID, candidate.PluginPrefix) {
			continue
		}
		score := len(candidate.PluginPrefix) * 2
		if candidate.Publisher != "" {
			score += 10_000
		}
		if score == bestScore {
			return Requirement{}, errors.New("来源证明 requirement 选择歧义")
		}
		if score > bestScore {
			best, bestScore, found = candidate, score, true
		}
	}
	if !found {
		return Requirement{}, errors.New("当前制品没有匹配的来源证明 requirement")
	}
	return best, nil
}

func matchesPluginPrefix(pluginID, prefix string) bool {
	if strings.HasSuffix(prefix, ".") {
		return strings.HasPrefix(pluginID, prefix)
	}
	return pluginID == prefix || strings.HasPrefix(pluginID, prefix+".")
}

func enforceRequirement(requirement Requirement, record VerificationRecord) error {
	if !slices.Contains(requirement.BuilderIDs, record.BuilderID) || !slices.Contains(requirement.BuildTypes, record.BuildType) {
		return errors.New("来源证明 builder/buildType 不符合策略")
	}
	if len(requirement.Issuers) > 0 && !slices.Contains(requirement.Issuers, record.Issuer) || len(requirement.Workflows) > 0 && !slices.Contains(requirement.Workflows, record.Workflow) {
		return errors.New("来源证明 issuer/workflow 不符合策略")
	}
	sourceMatched, digestFound := false, false
	for _, source := range record.Sources {
		for _, prefix := range requirement.SourceURIPrefixes {
			sourceMatched = sourceMatched || strings.HasPrefix(source.URI, prefix)
		}
		digestFound = digestFound || len(source.Digests) > 0
	}
	if !sourceMatched || requirement.RequireSourceDigest && !digestFound {
		return errors.New("来源证明 source URI/digest 不符合策略")
	}
	return nil
}

func sameSummary(left, right StatementSummary) bool {
	leftRaw, _ := json.Marshal(left)
	rightRaw, _ := json.Marshal(right)
	return string(leftRaw) == string(rightRaw)
}

func cloneSources(values []Source) []Source {
	result := make([]Source, len(values))
	for index, value := range values {
		result[index] = Source{URI: value.URI, Digests: append([]Digest(nil), value.Digests...)}
	}
	return result
}

func keyIdentity(providerID, keyID string) string { return providerID + "\x00" + keyID }
