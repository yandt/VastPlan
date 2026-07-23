package artifactprovenance

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

func InspectVerificationRecord(raw []byte) (VerificationRecord, string, error) {
	record, err := decodeRecord(raw)
	if err != nil {
		return VerificationRecord{}, "", err
	}
	digest := sha256.Sum256(raw)
	return record, hex.EncodeToString(digest[:]), nil
}

func SignRecord(record VerificationRecord, privateKey ed25519.PrivateKey) (VerificationRecord, error) {
	if len(privateKey) != ed25519.PrivateKeySize {
		return VerificationRecord{}, errors.New("Verifier 私钥不是 Ed25519")
	}
	record.SchemaVersion = RecordSchemaVersion
	record.Algorithm = "ed25519"
	record.Signature = ""
	if err := validateRecord(record); err != nil {
		return VerificationRecord{}, err
	}
	payload, err := recordPayload(record)
	if err != nil {
		return VerificationRecord{}, err
	}
	record.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, payload))
	return record, nil
}

func decodeRecord(raw []byte) (VerificationRecord, error) {
	if len(raw) == 0 || len(raw) > MaxRecordBytes {
		return VerificationRecord{}, errors.New("Provenance Verification Record 大小无效")
	}
	var record VerificationRecord
	if err := decodeStrict(raw, &record); err != nil {
		return VerificationRecord{}, err
	}
	if err := validateRecord(record); err != nil {
		return VerificationRecord{}, err
	}
	return record, nil
}

func verifyRecordSignature(record VerificationRecord, publicKey ed25519.PublicKey) error {
	if len(publicKey) != ed25519.PublicKeySize {
		return errors.New("Verifier 公钥无效")
	}
	signature, err := base64.StdEncoding.DecodeString(record.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return errors.New("Verification Record 签名编码无效")
	}
	payload, err := recordPayload(record)
	if err != nil {
		return err
	}
	if !ed25519.Verify(publicKey, payload, signature) {
		return errors.New("Verification Record 签名验证失败")
	}
	return nil
}

func recordPayload(record VerificationRecord) ([]byte, error) {
	record.Signature = ""
	return json.Marshal(record)
}

func validateRecord(record VerificationRecord) error {
	if record.SchemaVersion != RecordSchemaVersion || record.Algorithm != "ed25519" || !validSHA256(record.SubjectSHA256) || !validSHA256(record.ProvenanceSHA256) {
		return errors.New("Verification Record 版本、算法或摘要无效")
	}
	if record.PredicateType != SLSAProvenanceType || strings.TrimSpace(record.BuilderID) == "" || strings.TrimSpace(record.BuildType) == "" || strings.TrimSpace(record.ProviderID) == "" || strings.TrimSpace(record.KeyID) == "" || strings.TrimSpace(record.PolicyID) == "" {
		return errors.New("Verification Record 必填身份字段缺失")
	}
	for _, value := range []string{record.BuilderID, record.BuildType, record.ProviderID, record.KeyID, record.PolicyID, record.Issuer, record.Workflow} {
		if strings.TrimSpace(value) != value {
			return errors.New("Verification Record 身份字段必须规范化")
		}
	}
	if record.VerifiedAt.IsZero() || record.ExpiresAt.IsZero() || record.VerifiedAt.Location() != time.UTC || record.ExpiresAt.Location() != time.UTC || !record.ExpiresAt.After(record.VerifiedAt) {
		return errors.New("Verification Record 时间窗口无效")
	}
	if len(record.BuilderID) > 2048 || len(record.BuildType) > 2048 || len(record.ProviderID) > 160 || len(record.KeyID) > 160 || len(record.PolicyID) > 160 || len(record.Issuer) > 2048 || len(record.Workflow) > 2048 || len(record.Sources) > 1024 {
		return errors.New("Verification Record 字段超限")
	}
	for _, source := range record.Sources {
		normalized, err := normalizeSource(source.URI, digestMap(source.Digests))
		if err != nil || !sameSource(normalized, source) {
			return errors.New("Verification Record source 未规范化")
		}
	}
	return nil
}

func digestMap(values []Digest) map[string]string {
	result := make(map[string]string, len(values))
	for _, value := range values {
		if _, exists := result[value.Algorithm]; exists {
			return map[string]string{"": ""}
		}
		result[value.Algorithm] = value.Value
	}
	return result
}

func sameSource(left, right Source) bool {
	if left.URI != right.URI || len(left.Digests) != len(right.Digests) {
		return false
	}
	for index := range left.Digests {
		if left.Digests[index] != right.Digests[index] {
			return false
		}
	}
	return true
}
