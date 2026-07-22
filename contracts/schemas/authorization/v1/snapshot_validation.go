package authorizationv1

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

func ParseSignedPolicySnapshot(raw []byte) (SignedPolicySnapshot, error) {
	if len(raw) > MaxAuthorizationIRBytes {
		return SignedPolicySnapshot{}, fmt.Errorf("Signed Policy Snapshot 超过 %d bytes", MaxAuthorizationIRBytes)
	}
	if err := validateSchema(IRSchemaURL+"#/$defs/signedPolicySnapshot", raw); err != nil {
		return SignedPolicySnapshot{}, err
	}
	var snapshot SignedPolicySnapshot
	if err := decodeStrict(raw, &snapshot); err != nil {
		return SignedPolicySnapshot{}, err
	}
	if err := ValidatePolicySnapshot(snapshot.Payload); err != nil {
		return SignedPolicySnapshot{}, err
	}
	return snapshot, nil
}

// ValidatePolicySnapshot validates shape and time semantics only. Signature
// trust, audience, key rotation, and revocation freshness belong to the B4
// Enforcer and must not be inferred from this function.
func ValidatePolicySnapshot(snapshot PolicySnapshot) error {
	if snapshot.IssuedAt.IsZero() || snapshot.NotBefore.Before(snapshot.IssuedAt) || !snapshot.ExpiresAt.After(snapshot.NotBefore) {
		return errors.New("Policy Snapshot 时间窗无效")
	}
	return ValidateAuthorizationIR(snapshot.Policy)
}

// CanonicalPolicySnapshot defines the language-neutral bytes covered by the
// Ed25519 signature. Set-like audience values and the embedded IR are
// normalized before encoding.
func CanonicalPolicySnapshot(snapshot PolicySnapshot) ([]byte, error) {
	normalized := snapshot
	normalized.Audience = append([]string(nil), snapshot.Audience...)
	sort.Strings(normalized.Audience)
	normalized.IssuedAt = normalized.IssuedAt.UTC()
	normalized.NotBefore = normalized.NotBefore.UTC()
	normalized.ExpiresAt = normalized.ExpiresAt.UTC()
	policy, err := NormalizeAuthorizationIR(snapshot.Policy)
	if err != nil {
		return nil, err
	}
	normalized.Policy = policy
	var output bytes.Buffer
	encoder := json.NewEncoder(&output)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(normalized); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(output.Bytes(), []byte("\n")), nil
}
