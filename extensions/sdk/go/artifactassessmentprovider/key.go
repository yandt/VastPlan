package artifactassessmentprovider

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"strings"
)

func ParseEd25519PrivateKey(material []byte) (ed25519.PrivateKey, error) {
	if len(material) == ed25519.PrivateKeySize {
		return append(ed25519.PrivateKey(nil), material...), nil
	}
	if block, trailing := pem.Decode(material); block != nil {
		if len(strings.TrimSpace(string(trailing))) != 0 {
			return nil, errors.New("Ed25519 PEM 包含额外内容")
		}
		parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		key, ok := parsed.(ed25519.PrivateKey)
		if err != nil || !ok {
			return nil, errors.New("Material Lease 内容不是 Ed25519 PKCS#8 私钥")
		}
		return append(ed25519.PrivateKey(nil), key...), nil
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(material)))
	if err != nil || len(decoded) != ed25519.PrivateKeySize {
		return nil, errors.New("Material Lease 内容不是 Ed25519 私钥")
	}
	return append(ed25519.PrivateKey(nil), decoded...), nil
}

func ZeroPrivateKey(key ed25519.PrivateKey) {
	for index := range key {
		key[index] = 0
	}
}
