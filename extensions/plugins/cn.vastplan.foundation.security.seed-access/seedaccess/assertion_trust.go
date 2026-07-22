package seedaccess

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	authenticationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authentication/v1"
)

type AssertionProofVerifier interface {
	Verify(authenticationv1.SignedAuthenticationAssertion) error
}

type FileAssertionTrust struct{ Path string }

type assertionTrustDocument struct {
	Version int                 `json:"version"`
	Keys    []assertionTrustKey `json:"keys"`
}
type assertionTrustKey struct {
	KeyID     string `json:"keyId"`
	PublicKey string `json:"publicKey"`
}

func (v FileAssertionTrust) Verify(assertion authenticationv1.SignedAuthenticationAssertion) error {
	if !filepath.IsAbs(v.Path) || filepath.Clean(v.Path) != v.Path {
		return errors.New("Assertion trust 必须是规范绝对路径")
	}
	info, err := os.Lstat(v.Path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o022 != 0 {
		return errors.New("Assertion trust 文件权限不安全")
	}
	raw, err := os.ReadFile(v.Path)
	if err != nil {
		return err
	}
	var document assertionTrustDocument
	if err := json.Unmarshal(raw, &document); err != nil || document.Version != 1 || len(document.Keys) < 1 || len(document.Keys) > 16 {
		return errors.New("Assertion trust 文件无效")
	}
	canonical, err := authenticationv1.CanonicalAssertion(assertion.Payload)
	if err != nil {
		return err
	}
	signature, err := base64.RawURLEncoding.DecodeString(assertion.Signature.Value)
	if err != nil || len(signature) != ed25519.SignatureSize || assertion.Signature.Algorithm != "Ed25519" {
		return errors.New("Assertion signature 无效")
	}
	for _, key := range document.Keys {
		if key.KeyID != assertion.Signature.KeyID {
			continue
		}
		public, decodeErr := base64.RawStdEncoding.DecodeString(key.PublicKey)
		if decodeErr == nil && len(public) == ed25519.PublicKeySize && ed25519.Verify(ed25519.PublicKey(public), canonical, signature) {
			return nil
		}
	}
	return errors.New("Assertion signature 不受信")
}
