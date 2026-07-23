package artifactassessmentprovider

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"testing"
)

func TestParseEd25519PrivateKeyCopiesAndZeroesMaterial(t *testing.T) {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	raw := append([]byte(nil), privateKey...)
	parsed, err := ParseEd25519PrivateKey(raw)
	if err != nil {
		t.Fatal(err)
	}
	ZeroPrivateKey(parsed)
	if raw[0] == 0 {
		t.Fatal("清零解析结果不得依赖修改 Material Lease 输入切片")
	}
	encoded, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err = ParseEd25519PrivateKey(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded}))
	if err != nil || len(parsed) != ed25519.PrivateKeySize {
		t.Fatalf("PKCS#8 Ed25519 解析失败: %v", err)
	}
	ZeroPrivateKey(parsed)
}
