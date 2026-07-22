package broker

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	authenticationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authentication/v1"
)

type AssertionSigner interface {
	Sign(authenticationv1.AuthenticationAssertion) (authenticationv1.SignedAuthenticationAssertion, error)
}
type AssertionVerifier interface {
	Verify(authenticationv1.SignedAuthenticationAssertion) error
}

type Ed25519Assertions struct {
	KeyID   string
	Private ed25519.PrivateKey
	Public  ed25519.PublicKey
}

func (s Ed25519Assertions) Sign(value authenticationv1.AuthenticationAssertion) (authenticationv1.SignedAuthenticationAssertion, error) {
	if len(s.Private) != ed25519.PrivateKeySize || s.KeyID == "" {
		return authenticationv1.SignedAuthenticationAssertion{}, errors.New("Authentication Assertion 签名密钥无效")
	}
	raw, err := authenticationv1.CanonicalAssertion(value)
	if err != nil {
		return authenticationv1.SignedAuthenticationAssertion{}, err
	}
	signature := ed25519.Sign(s.Private, raw)
	return authenticationv1.SignedAuthenticationAssertion{Payload: value, Signature: authenticationv1.Signature{Algorithm: "Ed25519", KeyID: s.KeyID, Value: base64.RawURLEncoding.EncodeToString(signature)}}, nil
}

func (s Ed25519Assertions) Verify(value authenticationv1.SignedAuthenticationAssertion) error {
	if value.Signature.Algorithm != "Ed25519" || value.Signature.KeyID != s.KeyID || len(s.Public) != ed25519.PublicKeySize {
		return errors.New("Authentication Assertion 签名元数据无效")
	}
	signature, err := base64.RawURLEncoding.DecodeString(value.Signature.Value)
	if err != nil {
		return err
	}
	raw, err := authenticationv1.CanonicalAssertion(value.Payload)
	if err != nil {
		return err
	}
	if !ed25519.Verify(s.Public, raw, signature) {
		return errors.New("Authentication Assertion 签名无效")
	}
	return nil
}

type assertionKeyFile struct {
	KeyID      string `json:"keyId"`
	PrivateKey string `json:"privateKey"`
}

func LoadAssertionKey(path string) (Ed25519Assertions, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return Ed25519Assertions{}, errors.New("Assertion key 必须是规范绝对路径")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
		return Ed25519Assertions{}, errors.New("Assertion key 必须是 owner-only 普通文件")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return Ed25519Assertions{}, err
	}
	defer clear(raw)
	var document assertionKeyFile
	if err := json.Unmarshal(raw, &document); err != nil {
		return Ed25519Assertions{}, err
	}
	private, err := base64.RawStdEncoding.DecodeString(document.PrivateKey)
	if err != nil || len(private) != ed25519.PrivateKeySize {
		return Ed25519Assertions{}, errors.New("Assertion private key 无效")
	}
	key := ed25519.PrivateKey(private)
	return Ed25519Assertions{KeyID: document.KeyID, Private: key, Public: key.Public().(ed25519.PublicKey)}, nil
}

func GenerateAssertionKey(keyID string) (Ed25519Assertions, error) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	return Ed25519Assertions{KeyID: keyID, Private: private, Public: public}, err
}
