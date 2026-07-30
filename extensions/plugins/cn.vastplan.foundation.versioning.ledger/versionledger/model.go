package versionledger

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	versioningv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versioning/v1"
)

func validateProviderRequest(scope Scope, request *versioningv1.ProviderPutVersionRequest) error {
	if err := scope.Validate(); err != nil {
		return providerError(versioningv1.ErrorInvalidRequest, false, err)
	}
	raw, err := json.Marshal(request)
	if err != nil {
		return providerError(versioningv1.ErrorInvalidRequest, false, err)
	}
	parsed, err := versioningv1.ParseProviderRequest(versioningv1.ProviderOperationPutVersion, raw)
	if err != nil {
		return providerError(versioningv1.ErrorInvalidRequest, false, err)
	}
	*request = *parsed.(*versioningv1.ProviderPutVersionRequest)
	if err := versioningv1.ValidateDerivedVersionID(request.Candidate.VersionID, scope.TenantID, request.Candidate.Stream, request.IdempotencyKey); err != nil {
		return providerError(versioningv1.ErrorDigestMismatch, false, err)
	}
	return nil
}

func validateScopedRequest(scope Scope, operation string, request any) error {
	if err := scope.Validate(); err != nil {
		return providerError(versioningv1.ErrorInvalidRequest, false, err)
	}
	raw, err := json.Marshal(request)
	if err != nil {
		return providerError(versioningv1.ErrorInvalidRequest, false, err)
	}
	if _, err := versioningv1.ParseRequest(operation, raw); err != nil {
		return providerError(versioningv1.ErrorInvalidRequest, false, err)
	}
	return nil
}

func candidateDigest(candidate versioningv1.ProviderVersionCandidate) (string, error) {
	raw, err := json.Marshal(candidate)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func candidateFromRecord(record versioningv1.VersionRecord) versioningv1.ProviderVersionCandidate {
	return versioningv1.ProviderVersionCandidate{
		VersionID: record.Ref.VersionID, Stream: record.Ref.Stream, Parent: record.Parent, Content: append([]byte(nil), record.Content...),
		Message: record.Message, Labels: cloneLabels(record.Labels), ActorID: record.ActorID,
	}
}

func sameCandidate(record versioningv1.VersionRecord, candidate versioningv1.ProviderVersionCandidate) bool {
	if record.Ref.VersionID != candidate.VersionID || record.Ref.Stream != candidate.Stream || record.ActorID != candidate.ActorID || record.Message != candidate.Message || !equalVersionRef(record.Parent, candidate.Parent) {
		return false
	}
	leftContent, leftErr := versioningv1.CanonicalizeContent(record.Content)
	rightContent, rightErr := versioningv1.CanonicalizeContent(candidate.Content)
	if leftErr != nil || rightErr != nil || string(leftContent) != string(rightContent) {
		return false
	}
	return equalLabels(record.Labels, candidate.Labels)
}

func equalVersionRef(left, right *versioningv1.VersionRef) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func equalLabels(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}

func cloneRecord(record versioningv1.VersionRecord) versioningv1.VersionRecord {
	record.Content = append([]byte(nil), record.Content...)
	if record.Parent != nil {
		parent := *record.Parent
		record.Parent = &parent
	}
	if record.Labels != nil {
		record.Labels = make(map[string]string, len(record.Labels))
		for key, value := range record.Labels {
			record.Labels[key] = value
		}
	}
	return record
}

func requireStoredParent(versions map[string]versioningv1.VersionRecord, sequence uint64, candidate versioningv1.ProviderVersionCandidate) error {
	if candidate.Parent == nil {
		if sequence != 1 {
			return providerError(versioningv1.ErrorConflict, false, errors.New("非首个版本必须引用父版本"))
		}
		return nil
	}
	stored, ok := versions[candidate.Parent.VersionID]
	if !ok || stored.Ref != *candidate.Parent {
		return providerError(versioningv1.ErrorNotFound, false, errors.New("父版本不存在或引用不精确"))
	}
	return nil
}
