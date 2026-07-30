package versioningv1

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"unicode/utf8"
)

const VersionIdentityAlgorithm = "version.identity.v1"

// DeriveVersionID returns the protocol-independent logical version identity.
// The NUL separators are unambiguous because every input is validated to
// exclude NUL before hashing.
func DeriveVersionID(tenantID string, stream StreamKey, idempotencyKey string) (string, error) {
	if err := ValidateVersionIdentityTenant(tenantID); err != nil {
		return "", err
	}
	if err := ValidateStreamKey(stream); err != nil {
		return "", err
	}
	if !idempotencyPattern.MatchString(idempotencyKey) {
		return "", errors.New("Version identity idempotencyKey 无效")
	}
	preimage := VersionIdentityAlgorithm + "\x00" + tenantID + "\x00" + stream.Namespace + "\x00" + stream.StreamID + "\x00" + idempotencyKey
	sum := sha256.Sum256([]byte(preimage))
	return hex.EncodeToString(sum[:]), nil
}

// ValidateVersionIdentityTenant applies the identity algorithm's opaque tenant
// boundary without imposing product-specific tenant naming rules.
func ValidateVersionIdentityTenant(tenantID string) error {
	if tenantID == "" || len(tenantID) > 256 || !utf8.ValidString(tenantID) || strings.TrimSpace(tenantID) != tenantID || strings.ContainsRune(tenantID, '\x00') {
		return errors.New("Version identity tenant 无效")
	}
	return nil
}

// ValidateDerivedVersionID verifies a logical ID without trusting the
// Provider's storage-specific identity.
func ValidateDerivedVersionID(versionID, tenantID string, stream StreamKey, idempotencyKey string) error {
	expected, err := DeriveVersionID(tenantID, stream, idempotencyKey)
	if err != nil {
		return err
	}
	if versionID != expected {
		return errors.New("versionId 不符合 version.identity.v1")
	}
	return nil
}
