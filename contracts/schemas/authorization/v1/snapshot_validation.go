package authorizationv1

import (
	"errors"
	"fmt"
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
