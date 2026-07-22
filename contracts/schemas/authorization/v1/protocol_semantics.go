package authorizationv1

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

func validateProviderSemantics(_, _ string, message any, _ bool) error {
	switch typed := message.(type) {
	case *StoreCompareAndSwapRequest:
		if typed.ExpectedRevision == ^uint64(0) || typed.Document.Revision != typed.ExpectedRevision+1 {
			return errors.New("Store CAS document.revision 必须等于 expectedRevision + 1")
		}
		return validateStoreDocument(typed.Document)
	case *StoreLoadResult:
		if typed.Found != (typed.Document != nil) {
			return errors.New("Store load found 必须与 document 是否存在一致")
		}
		if typed.Document != nil {
			return validateStoreDocument(*typed.Document)
		}
	case *StoreAppendAuditRequest:
		if len(typed.Record.Details) > MaxAuditDetailsBytes {
			return fmt.Errorf("Audit details 超过 %d bytes", MaxAuditDetailsBytes)
		}
		if sensitiveJSONKey(typed.Record.Details) != "" {
			return errors.New("Audit details 不得包含秘密或 material 字段")
		}
	case *EnginePrepareRequest:
		return ValidatePolicySnapshot(typed.Snapshot.Payload)
	case *EngineEvaluateRequest:
		return validateEvaluationInput(typed.Input)
	case *EngineEvaluateResult:
		if typed.Decision != typed.Proof.Decision {
			return errors.New("Engine decision 与 proof.decision 不一致")
		}
		if typed.Proof.EvaluatedAt.IsZero() || !typed.Proof.ValidUntil.After(typed.Proof.EvaluatedAt) || typed.Proof.ValidUntil.Sub(typed.Proof.EvaluatedAt) > 5*time.Minute {
			return errors.New("Decision Proof 有效期必须在 (0, 5m] 内")
		}
	case *DirectoryResolveSubjectResult:
		if typed.Subject.Issuer == "" {
			return errors.New("Directory 返回的 Subject 必须绑定 issuer")
		}
	case *DirectoryResolveGroupsResult:
		seen := map[string]struct{}{}
		for _, group := range typed.Groups {
			key := group.Issuer + "\x00" + group.ID
			if _, duplicate := seen[key]; duplicate {
				return errors.New("Directory 返回了重复 Group")
			}
			seen[key] = struct{}{}
		}
	case *ExchangePlanImportRequest:
		return validateExchangeDocument(typed.Document)
	case *ExchangeImportResult:
		return validateImportProposal(typed.Proposal)
	case *ExchangeExportResult:
		return validateExchangeDocument(typed.Document)
	}
	return nil
}

func validateStoreDocument(document StoreDocument) error {
	if len(document.Content) > MaxStoreDocumentBytes {
		return fmt.Errorf("Store document content 超过 %d bytes", MaxStoreDocumentBytes)
	}
	digest, err := DigestRawDocument(document.Content)
	if err != nil {
		return fmt.Errorf("Store document content 无效: %w", err)
	}
	if digest != document.Digest {
		return errors.New("Store document digest 与 content 不匹配")
	}
	return nil
}

func validateExchangeDocument(document ExchangeDocument) error {
	if len(document.Content) > MaxExchangeDocumentBytes {
		return fmt.Errorf("Exchange document content 超过 %d bytes", MaxExchangeDocumentBytes)
	}
	digest, err := DigestRawDocument(document.Content)
	if err != nil {
		return fmt.Errorf("Exchange document content 无效: %w", err)
	}
	if digest != document.Digest {
		return errors.New("Exchange document digest 与 content 不匹配")
	}
	return nil
}

func validateEvaluationInput(input EvaluationInput) error {
	if input.EvaluatedAt.IsZero() {
		return errors.New("Evaluation input 缺少可信 evaluatedAt")
	}
	if input.Scope.ProjectID != "" && input.Scope.TenantID == "" {
		return errors.New("Evaluation project scope 必须包含 tenant")
	}
	if (input.Scope.ResourceType == "") != (input.Scope.ResourceID == "") {
		return errors.New("Evaluation resourceType/resourceId 必须同时出现")
	}
	return nil
}

func validateImportProposal(proposal PolicyImportProposal) error {
	roles := map[string]struct{}{}
	for _, role := range proposal.Roles {
		if role.DomainID != proposal.DomainID {
			return errors.New("Exchange Provider 不得向其他 Domain 注入 Role")
		}
		roles[role.ID+"@"+fmt.Sprint(role.Revision)] = struct{}{}
	}
	for _, binding := range proposal.Bindings {
		if binding.DomainID != proposal.DomainID {
			return errors.New("Exchange Provider 不得向其他 Domain 注入 Binding")
		}
		if _, exists := roles[binding.RoleID+"@"+fmt.Sprint(binding.RoleRevision)]; !exists {
			return errors.New("Exchange Binding 必须引用同一 Proposal 内的精确 Role revision")
		}
		if !binding.ExpiresAt.After(binding.NotBefore) {
			return errors.New("Exchange Binding 时间窗无效")
		}
	}
	return nil
}

func sensitiveJSONKey(raw json.RawMessage) string {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return "invalid"
	}
	return findSensitiveKey(value)
}

func findSensitiveKey(value any) string {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			lower := strings.ToLower(key)
			for _, marker := range []string{"password", "secret", "token", "material", "privatekey", "credential"} {
				if strings.Contains(lower, marker) {
					return key
				}
			}
			if nested := findSensitiveKey(child); nested != "" {
				return nested
			}
		}
	case []any:
		for _, child := range typed {
			if nested := findSensitiveKey(child); nested != "" {
				return nested
			}
		}
	}
	return ""
}
