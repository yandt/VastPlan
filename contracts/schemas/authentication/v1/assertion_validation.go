package authenticationv1

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"time"
)

func ParseSignedAssertion(raw []byte) (SignedAuthenticationAssertion, error) {
	if len(raw) > MaxAssertionBytes {
		return SignedAuthenticationAssertion{}, errors.New("Signed Authentication Assertion 超过大小上限")
	}
	if err := validateSchema(AssertionSchemaURL, raw); err != nil {
		return SignedAuthenticationAssertion{}, err
	}
	var assertion SignedAuthenticationAssertion
	if err := decodeStrict(raw, &assertion); err != nil {
		return SignedAuthenticationAssertion{}, err
	}
	if err := validateAssertionSemantics(assertion.Payload); err != nil {
		return SignedAuthenticationAssertion{}, err
	}
	return assertion, nil
}

// ValidateAssertion validates shape and bounded lifetime only. The Node Portal
// Kernel must still verify signature, audience, transaction nonce, one-time use,
// and Access Profile binding before issuing a browser session.
func ValidateAssertion(assertion AuthenticationAssertion) error {
	raw, err := json.Marshal(assertion)
	if err != nil {
		return err
	}
	if err := validateSchema(AssertionSchemaURL+"#/$defs/assertion", raw); err != nil {
		return err
	}
	return validateAssertionSemantics(assertion)
}

func validateAssertionSemantics(assertion AuthenticationAssertion) error {
	if assertion.IssuedAt.IsZero() || !assertion.ExpiresAt.After(assertion.IssuedAt) || assertion.ExpiresAt.Sub(assertion.IssuedAt) > 30*time.Second {
		return errors.New("Authentication Assertion 有效期必须在 (0, 30s] 内")
	}
	return nil
}

func CanonicalAssertion(assertion AuthenticationAssertion) ([]byte, error) {
	clone := assertion
	clone.IssuedAt = clone.IssuedAt.UTC()
	clone.ExpiresAt = clone.ExpiresAt.UTC()
	clone.AMR = append([]string(nil), clone.AMR...)
	sort.Strings(clone.AMR)
	var output bytes.Buffer
	encoder := json.NewEncoder(&output)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(clone); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(output.Bytes(), []byte("\n")), nil
}

func AssertionDigest(assertion AuthenticationAssertion) (string, error) {
	raw, err := CanonicalAssertion(assertion)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}
