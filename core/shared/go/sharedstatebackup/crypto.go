package sharedstatebackup

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
)

func GenerateSigningKey(keyID string) (ed25519.PrivateKey, TrustDocument, error) {
	if !safeName(keyID) {
		return nil, TrustDocument{}, errors.New("Shared State 备份 key ID 无效")
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, TrustDocument{}, err
	}
	trust := TrustDocument{Format: TrustFormat, Keys: []TrustKey{{KeyID: keyID, Algorithm: "Ed25519", PublicKey: base64.RawURLEncoding.EncodeToString(public)}}}
	return private, trust, nil
}

func MarshalPrivateKeyPEM(private ed25519.PrivateKey) ([]byte, error) {
	if len(private) != ed25519.PrivateKeySize {
		return nil, errors.New("Shared State 备份 Ed25519 私钥无效")
	}
	der, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), nil
}

func LoadPrivateKeyPEM(filename string) (ed25519.PrivateKey, error) {
	info, err := os.Lstat(filename)
	if err != nil {
		return nil, fmt.Errorf("读取 Shared State 备份私钥属性: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 || info.Size() < 1 || info.Size() > MaxPrivateKeyBytes {
		return nil, errors.New("Shared State 备份私钥必须是 0600 或更严格的普通文件")
	}
	raw, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	block, trailing := pem.Decode(raw)
	if block == nil || block.Type != "PRIVATE KEY" || len(trailing) != 0 {
		return nil, errors.New("Shared State 备份私钥 PEM 无效")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	private, ok := parsed.(ed25519.PrivateKey)
	if !ok || len(private) != ed25519.PrivateKeySize {
		return nil, errors.New("Shared State 备份私钥不是 Ed25519")
	}
	return private, nil
}

func MarshalTrustDocument(value TrustDocument) ([]byte, error) {
	if err := validateTrustDocument(value); err != nil {
		return nil, err
	}
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(raw, '\n'), nil
}

func LoadTrustDocument(filename string) (TrustDocument, error) {
	info, err := os.Lstat(filename)
	if err != nil {
		return TrustDocument{}, fmt.Errorf("读取 Shared State 备份信任文档属性: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o022 != 0 || info.Size() < 1 || info.Size() > MaxTrustBytes {
		return TrustDocument{}, errors.New("Shared State 备份信任文档类型或大小无效")
	}
	raw, err := os.ReadFile(filename)
	if err != nil {
		return TrustDocument{}, fmt.Errorf("读取 Shared State 备份信任文档: %w", err)
	}
	var value TrustDocument
	if err := decodeStrictJSON(raw, &value); err != nil {
		return TrustDocument{}, err
	}
	return value, validateTrustDocument(value)
}

func validateTrustDocument(value TrustDocument) error {
	if value.Format != TrustFormat || len(value.Keys) == 0 || len(value.Keys) > 32 {
		return errors.New("Shared State 备份信任文档无效")
	}
	seen := map[string]struct{}{}
	for _, key := range value.Keys {
		public, err := base64.RawURLEncoding.DecodeString(key.PublicKey)
		if !safeName(key.KeyID) || key.Algorithm != "Ed25519" || err != nil || len(public) != ed25519.PublicKeySize {
			return errors.New("Shared State 备份信任密钥无效")
		}
		if _, exists := seen[key.KeyID]; exists {
			return errors.New("Shared State 备份信任 key ID 重复")
		}
		seen[key.KeyID] = struct{}{}
	}
	return nil
}
