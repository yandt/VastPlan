package artifactprovenance

import (
	"encoding/json"
	"errors"
	"strings"
)

const ProviderCapabilityPrefix = "platform.artifacts.provenance."

// VerifyRequest is the stable external Provider input. It carries no package
// bytes, repository token, signing key or execution capability.
type VerifyRequest struct {
	SubjectSHA256 string          `json:"subjectSha256"`
	PolicyID      string          `json:"policyId"`
	Provenance    json.RawMessage `json:"provenance"`
}

type VerifyResult struct {
	Record json.RawMessage `json:"record"`
}

func ValidateVerifyRequest(request VerifyRequest) error {
	if !validSHA256(request.SubjectSHA256) || request.PolicyID == "" || strings.TrimSpace(request.PolicyID) != request.PolicyID || len(request.PolicyID) > 160 {
		return errors.New("Provenance Provider 请求 subject/policy 无效")
	}
	if len(request.Provenance) == 0 || len(request.Provenance) > MaxProvenanceBytes {
		return errors.New("Provenance Provider 请求原文缺失或超限")
	}
	return nil
}

func ValidateVerifyResult(result VerifyResult, subjectSHA256, policyID string) error {
	record, _, err := InspectVerificationRecord(result.Record)
	if err != nil {
		return err
	}
	if record.SubjectSHA256 != subjectSHA256 || record.PolicyID != policyID {
		return errors.New("Provenance Provider 回执未绑定请求 subject/policy")
	}
	return nil
}
