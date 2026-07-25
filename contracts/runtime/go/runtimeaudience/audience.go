// Package runtimeaudience defines the opaque, non-secret audience identifier
// carried between a trusted Runtime Host and a plugin SDK. It deliberately
// exposes no RuntimeIdentity constructor or host provenance.
package runtimeaudience

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
)

const Prefix = "runtime:v1:"

func FromDigest(digest [sha256.Size]byte) string {
	return Prefix + base64.RawURLEncoding.EncodeToString(digest[:])
}

func Validate(audience string) error {
	if !strings.HasPrefix(audience, Prefix) {
		return errors.New("runtime audience 前缀无效")
	}
	encoded := strings.TrimPrefix(audience, Prefix)
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(raw) != sha256.Size || base64.RawURLEncoding.EncodeToString(raw) != encoded {
		return errors.New("runtime audience 摘要无效")
	}
	return nil
}
