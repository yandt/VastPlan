package artifactassessment

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
)

func SignAdmission(record AdmissionRecord, privateKey ed25519.PrivateKey) (AdmissionRecord, error) {
	if len(privateKey) != ed25519.PrivateKeySize {
		return AdmissionRecord{}, errors.New("安全评估 Provider 私钥不是 Ed25519")
	}
	record.SchemaVersion, record.Algorithm, record.Signature = SchemaVersion, "ed25519", ""
	if err := validateAdmission(record); err != nil {
		return AdmissionRecord{}, err
	}
	payload, _ := json.Marshal(record)
	record.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, payload))
	return record, nil
}

func SignStatus(record StatusRecord, privateKey ed25519.PrivateKey) (StatusRecord, error) {
	if len(privateKey) != ed25519.PrivateKeySize {
		return StatusRecord{}, errors.New("安全评估 Provider 私钥不是 Ed25519")
	}
	record.SchemaVersion, record.Algorithm, record.Signature = SchemaVersion, "ed25519", ""
	if err := validateStatus(record); err != nil {
		return StatusRecord{}, err
	}
	payload, _ := json.Marshal(record)
	record.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, payload))
	return record, nil
}

func InspectAdmission(raw []byte) (AdmissionRecord, string, error) {
	var record AdmissionRecord
	if err := decodeRecord(raw, &record); err != nil {
		return record, "", err
	}
	if err := validateAdmission(record); err != nil {
		return record, "", err
	}
	return record, digest(raw), nil
}

func InspectStatus(raw []byte) (StatusRecord, string, error) {
	var record StatusRecord
	if err := decodeRecord(raw, &record); err != nil {
		return record, "", err
	}
	if err := validateStatus(record); err != nil {
		return record, "", err
	}
	return record, digest(raw), nil
}

func verifyAdmissionSignature(record AdmissionRecord, key ed25519.PublicKey) error {
	signature := record.Signature
	record.Signature = ""
	return verifySignature(record, signature, key)
}

func verifyStatusSignature(record StatusRecord, key ed25519.PublicKey) error {
	signature := record.Signature
	record.Signature = ""
	return verifySignature(record, signature, key)
}

func verifySignature(record any, encoded string, key ed25519.PublicKey) error {
	signature, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(signature) != ed25519.SignatureSize || len(key) != ed25519.PublicKeySize {
		return errors.New("安全评估记录签名编码无效")
	}
	payload, err := json.Marshal(record)
	if err != nil || !ed25519.Verify(key, payload, signature) {
		return errors.New("安全评估记录签名验证失败")
	}
	return nil
}

func validateAdmission(record AdmissionRecord) error {
	if record.SchemaVersion != SchemaVersion || record.Algorithm != "ed25519" {
		return errors.New("安全准入记录版本或算法无效")
	}
	if err := validateIdentityFields(record.ProviderID, record.KeyID, record.PolicyID); err != nil {
		return err
	}
	return validateEvaluation(record.Evaluation)
}

func validateStatus(record StatusRecord) error {
	if record.SchemaVersion != SchemaVersion || record.Algorithm != "ed25519" || record.Sequence == 0 || !validSHA256(record.AdmissionSHA256) || !validSHA256(record.PreviousSHA256) {
		return errors.New("安全复扫状态版本、算法或链位置无效")
	}
	if err := validateIdentityFields(record.ProviderID, record.KeyID, record.PolicyID); err != nil {
		return err
	}
	return validateEvaluation(record.Evaluation)
}

func validateIdentityFields(values ...string) error {
	for _, value := range values {
		if value == "" || strings.TrimSpace(value) != value || len(value) > 160 {
			return errors.New("安全评估记录身份字段无效")
		}
	}
	return nil
}

func validateEvaluation(value Evaluation) error {
	if !validSHA256(value.SubjectSHA256) || !validSHA256(value.SBOMSHA256) || value.Scanner.ID == "" || value.Scanner.Version == "" || value.Scanner.DatabaseRevision == "" {
		return errors.New("安全评估对象、SBOM 或扫描器身份无效")
	}
	for _, item := range []string{value.Scanner.ID, value.Scanner.Version, value.Scanner.DatabaseRevision} {
		if strings.TrimSpace(item) != item || len(item) > 256 {
			return errors.New("扫描器身份字段未规范化或超限")
		}
	}
	if value.Decision != DecisionPass && value.Decision != DecisionFail {
		return errors.New("安全评估 decision 必须是 pass 或 fail")
	}
	if value.EvaluatedAt.IsZero() || value.ExpiresAt.IsZero() || value.EvaluatedAt.Location() != time.UTC || value.ExpiresAt.Location() != time.UTC || !value.ExpiresAt.After(value.EvaluatedAt) {
		return errors.New("安全评估时间窗口无效")
	}
	for _, report := range []string{value.Vulnerabilities.ReportSHA256, value.Licenses.ReportSHA256} {
		if report != "" && !validSHA256(report) {
			return errors.New("安全评估报告摘要无效")
		}
	}
	return nil
}

func decodeRecord(raw []byte, target any) error {
	if len(raw) == 0 || len(raw) > MaxRecordBytes {
		return errors.New("安全评估记录大小无效")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("安全评估 JSON 只能包含一个顶层值")
		}
		return err
	}
	return nil
}

func digest(raw []byte) string {
	value := sha256.Sum256(raw)
	return hex.EncodeToString(value[:])
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
