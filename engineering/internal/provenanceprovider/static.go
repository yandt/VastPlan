// Package provenanceprovider contains the first external provenance verifier
// implementation. It is release tooling, not a repository or kernel trust root.
package provenanceprovider

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"cdsoft.com.cn/VastPlan/core/shared/go/artifactprovenance"
)

type BuilderKey struct {
	KeyID     string     `json:"keyId"`
	PublicKey string     `json:"publicKey"`
	NotBefore *time.Time `json:"notBefore,omitempty"`
	NotAfter  *time.Time `json:"notAfter,omitempty"`
	Revoked   bool       `json:"revoked,omitempty"`
}

type BuilderTrustDocument struct {
	SchemaVersion string       `json:"schemaVersion"`
	Keys          []BuilderKey `json:"keys"`
}

type Policy struct {
	SchemaVersion       string   `json:"schemaVersion"`
	ID                  string   `json:"id"`
	BuilderIDs          []string `json:"builderIds"`
	BuildTypes          []string `json:"buildTypes"`
	SourceURIPrefixes   []string `json:"sourceUriPrefixes"`
	RequireSourceDigest bool     `json:"requireSourceDigest"`
	RecordTTLHours      int      `json:"recordTtlHours"`
}

type Options struct {
	SubjectSHA256 string
	Provenance    []byte
	BuilderTrust  BuilderTrustDocument
	Policy        Policy
	ProviderID    string
	ProviderKeyID string
	ProviderKey   ed25519.PrivateKey
	Now           time.Time
}

func Verify(options Options) (artifactprovenance.VerificationRecord, error) {
	if options.Now.IsZero() {
		options.Now = time.Now().UTC()
	}
	if options.Now.Location() != time.UTC || options.ProviderID == "" || strings.TrimSpace(options.ProviderID) != options.ProviderID || options.ProviderKeyID == "" || strings.TrimSpace(options.ProviderKeyID) != options.ProviderKeyID || len(options.ProviderKey) != ed25519.PrivateKeySize {
		return artifactprovenance.VerificationRecord{}, errors.New("静态 Verifier Provider 身份、密钥或可信时钟无效")
	}
	if err := validatePolicy(options.Policy); err != nil {
		return artifactprovenance.VerificationRecord{}, err
	}
	keys, err := trustedBuilderKeys(options.BuilderTrust, options.Now)
	if err != nil {
		return artifactprovenance.VerificationRecord{}, err
	}
	summary, provenanceSHA, err := artifactprovenance.VerifyDSSEEd25519(options.Provenance, options.SubjectSHA256, keys)
	if err != nil {
		return artifactprovenance.VerificationRecord{}, err
	}
	if !slices.Contains(options.Policy.BuilderIDs, summary.BuilderID) || !slices.Contains(options.Policy.BuildTypes, summary.BuildType) {
		return artifactprovenance.VerificationRecord{}, errors.New("DSSE builder/buildType 不符合 Provider policy")
	}
	sourceMatch, digestFound := false, false
	for _, source := range summary.Sources {
		for _, prefix := range options.Policy.SourceURIPrefixes {
			sourceMatch = sourceMatch || strings.HasPrefix(source.URI, prefix)
		}
		digestFound = digestFound || len(source.Digests) > 0
	}
	if !sourceMatch || options.Policy.RequireSourceDigest && !digestFound {
		return artifactprovenance.VerificationRecord{}, errors.New("DSSE source URI/digest 不符合 Provider policy")
	}
	return artifactprovenance.SignRecord(artifactprovenance.VerificationRecord{
		SubjectSHA256: options.SubjectSHA256, ProvenanceSHA256: provenanceSHA, StatementSummary: summary,
		ProviderID: options.ProviderID, KeyID: options.ProviderKeyID, PolicyID: options.Policy.ID,
		VerifiedAt: options.Now, ExpiresAt: options.Now.Add(time.Duration(options.Policy.RecordTTLHours) * time.Hour),
	}, options.ProviderKey)
}

func DecodeBuilderTrust(raw []byte) (BuilderTrustDocument, error) {
	var value BuilderTrustDocument
	if err := decodeStrict(raw, &value); err != nil {
		return BuilderTrustDocument{}, err
	}
	return value, nil
}

func DecodePolicy(raw []byte) (Policy, error) {
	var value Policy
	if err := decodeStrict(raw, &value); err != nil {
		return Policy{}, err
	}
	return value, nil
}

func trustedBuilderKeys(document BuilderTrustDocument, now time.Time) (map[string]ed25519.PublicKey, error) {
	if document.SchemaVersion != "v1" || len(document.Keys) == 0 || len(document.Keys) > 128 {
		return nil, errors.New("Builder trust 文档版本或 key 数量无效")
	}
	result := make(map[string]ed25519.PublicKey, len(document.Keys))
	for _, key := range document.Keys {
		if key.NotBefore != nil && key.NotBefore.Location() != time.UTC || key.NotAfter != nil && key.NotAfter.Location() != time.UTC || key.NotBefore != nil && key.NotAfter != nil && !key.NotBefore.Before(*key.NotAfter) {
			return nil, fmt.Errorf("Builder key %s 时间窗口无效", key.KeyID)
		}
		if key.KeyID == "" || strings.TrimSpace(key.KeyID) != key.KeyID || key.Revoked || key.NotBefore != nil && now.Before(*key.NotBefore) || key.NotAfter != nil && now.After(*key.NotAfter) {
			continue
		}
		decoded, err := base64.StdEncoding.DecodeString(key.PublicKey)
		if err != nil || len(decoded) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("Builder key %s 无效", key.KeyID)
		}
		if _, exists := result[key.KeyID]; exists {
			return nil, fmt.Errorf("Builder key 重复: %s", key.KeyID)
		}
		result[key.KeyID] = append(ed25519.PublicKey(nil), decoded...)
	}
	if len(result) == 0 {
		return nil, errors.New("Builder trust 没有当前有效 key")
	}
	return result, nil
}

func validatePolicy(policy Policy) error {
	if policy.SchemaVersion != "v1" || policy.ID == "" || strings.TrimSpace(policy.ID) != policy.ID || policy.RecordTTLHours < 1 || policy.RecordTTLHours > 87_600 || len(policy.BuilderIDs) == 0 || len(policy.BuildTypes) == 0 || len(policy.SourceURIPrefixes) == 0 {
		return errors.New("静态 Verifier policy 版本、ID、有效期或允许列表无效")
	}
	for _, values := range [][]string{policy.BuilderIDs, policy.BuildTypes, policy.SourceURIPrefixes} {
		if len(values) > 128 {
			return errors.New("静态 Verifier policy 允许列表超限")
		}
		for index, value := range values {
			if value == "" || strings.TrimSpace(value) != value || slices.Contains(values[:index], value) {
				return errors.New("静态 Verifier policy 允许列表必须规范且不重复")
			}
		}
	}
	return nil
}

func decodeStrict(raw []byte, target any) error {
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("JSON 包含多余内容")
	}
	return nil
}
