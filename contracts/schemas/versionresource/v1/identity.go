package versionresourcev1

import (
	"encoding/json"
	"sort"

	versioningv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versioning/v1"
)

// CanonicalContent returns the exact JSON object submitted to Version Ledger.
// Snapshot adapters in every language must produce byte-identical output.
func CanonicalContent(snapshot Snapshot, maxBytes int64) (json.RawMessage, error) {
	if err := ValidateSnapshot(snapshot, maxBytes); err != nil {
		return nil, err
	}
	if snapshot.Kind == ContentJSON {
		return versioningv1.CanonicalizeContent(snapshot.JSON)
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return nil, err
	}
	return versioningv1.CanonicalizeContent(raw)
}

func SnapshotDigest(snapshot Snapshot, maxBytes int64) (string, error) {
	content, err := CanonicalContent(snapshot, maxBytes)
	if err != nil {
		return "", err
	}
	return versioningv1.ContentDigest(content)
}

// EnvironmentDigest binds a Session to one exact trusted routing, adapter and
// quota profile without exposing the underlying storage Provider.
func EnvironmentDigest(profile EnvironmentProfile) (string, error) {
	if err := ValidateEnvironmentProfile(profile); err != nil {
		return "", err
	}
	normalized := profile
	normalized.Bindings = append([]ResourceBinding(nil), profile.Bindings...)
	for index := range normalized.Bindings {
		normalized.Bindings[index].AllowedModes = append([]string(nil), normalized.Bindings[index].AllowedModes...)
		sort.Strings(normalized.Bindings[index].AllowedModes)
	}
	sort.Slice(normalized.Bindings, func(left, right int) bool {
		return normalized.Bindings[left].ResourceType < normalized.Bindings[right].ResourceType
	})
	raw, err := json.Marshal(normalized)
	if err != nil {
		return "", err
	}
	canonical, err := versioningv1.CanonicalizeContent(raw)
	if err != nil {
		return "", err
	}
	return versioningv1.ContentDigest(canonical)
}
