package pluginservice

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAttestation_SignVerifyRotateAndRevoke(t *testing.T) {
	packageBytes, artifact := testArtifact(t)
	publicKey, privateKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	signedAt := time.Date(2026, 7, 16, 3, 0, 0, 0, time.UTC)
	trust, err := NewTrustStore(TrustDocumentForPublicKeys(TrustKey{
		Publisher: "example", KeyID: "release-2026-01",
		PublicKey: base64.StdEncoding.EncodeToString(publicKey),
	}))
	if err != nil {
		t.Fatal(err)
	}
	attestation, err := SignArtifact(artifact, "example", "release-2026-01", privateKey, signedAt)
	if err != nil {
		t.Fatal(err)
	}
	if err := trust.Verify(attestation); err != nil {
		t.Fatalf("可信签名应通过: %v", err)
	}
	if err := ValidateArtifact(attestation.Artifact, packageBytes); err != nil {
		t.Fatalf("签名对应制品应通过内容校验: %v", err)
	}

	tampered := attestation
	tampered.Artifact.Channel = "canary"
	if err := trust.Verify(tampered); err == nil {
		t.Fatal("证明字段被改写后必须拒绝")
	}
	revoked, err := NewTrustStore(TrustDocumentForPublicKeys(TrustKey{
		Publisher: "example", KeyID: "release-2026-01", Revoked: true,
		PublicKey: base64.StdEncoding.EncodeToString(publicKey),
	}))
	if err != nil {
		t.Fatal(err)
	}
	if err := revoked.Verify(attestation); err == nil {
		t.Fatal("已撤销密钥签署的制品必须 fail-closed")
	}
}

func TestAttestation_ExpiredKeyCannotBackdateNewArtifact(t *testing.T) {
	_, artifact := testArtifact(t)
	publicKey, privateKey, _ := ed25519.GenerateKey(nil)
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	notAfter := now.Add(-time.Hour)
	trust, err := NewTrustStore(TrustDocumentForPublicKeys(TrustKey{
		Publisher: "example", KeyID: "expired", PublicKey: base64.StdEncoding.EncodeToString(publicKey), NotAfter: &notAfter,
	}))
	if err != nil {
		t.Fatal(err)
	}
	backdated, err := SignArtifact(artifact, "example", "expired", privateKey, notAfter.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if err := trust.verifyAt(backdated, now); err == nil {
		t.Fatal("已过期私钥不得通过回填 signedAt 恢复签署权限")
	}
	future, err := SignArtifact(artifact, "example", "expired", privateKey, now.Add(maximumSigningClockSkew+time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if err := trust.verifyAt(future, now); err == nil {
		t.Fatal("未来签署时间超过时钟偏差必须拒绝")
	}
}

func TestSignedRepository_RequiresAttestationAndKeepsItImmutable(t *testing.T) {
	packageBytes, artifact := testArtifact(t)
	publicKey, privateKey, _ := ed25519.GenerateKey(nil)
	trust, _ := NewTrustStore(TrustDocumentForPublicKeys(TrustKey{
		Publisher: "example", KeyID: "primary", PublicKey: base64.StdEncoding.EncodeToString(publicKey),
	}))
	local, _ := NewRepository(filepath.Join(t.TempDir(), "repository"))
	repository := &SignedRepository{Local: local, Trust: trust}
	attestation, _ := SignArtifact(artifact, "example", "primary", privateKey, time.Now().UTC())
	if _, err := repository.Publish(attestation, packageBytes); err != nil {
		t.Fatalf("发布可信制品失败: %v", err)
	}
	ref := Ref{PluginID: artifact.PluginID, Version: artifact.Version, Channel: artifact.Channel}
	if _, _, err := repository.Read(ref); err != nil {
		t.Fatalf("读取可信制品失败: %v", err)
	}
	envelope, err := repository.Fetch(context.Background(), ref)
	if err != nil || len(envelope.Proof) == 0 {
		t.Fatalf("签名种子源必须返回未信任证明供内核复验: proof=%d err=%v", len(envelope.Proof), err)
	}

	different, _ := SignArtifact(artifact, "example", "primary", privateKey, time.Now().UTC().Add(time.Second))
	if _, err := repository.Publish(different, packageBytes); err == nil {
		t.Fatal("同一不可变制品不得替换为不同证明")
	}
	dir, _ := local.artifactDir(ref)
	if err := os.Remove(filepath.Join(dir, "attestation.json")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := repository.Read(ref); err == nil {
		t.Fatal("签名证明缺失时必须拒绝读取")
	}
}

func TestPrivateKeyPEMRequiresOwnerOnlyPermissions(t *testing.T) {
	_, privateKey, _ := ed25519.GenerateKey(nil)
	raw, err := MarshalEd25519PrivateKeyPEM(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(t.TempDir(), "release-key.pem")
	if err := os.WriteFile(filename, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadEd25519PrivateKeyPEM(filename); err == nil {
		t.Fatal("宽权限私钥必须被拒绝")
	}
	if err := os.Chmod(filename, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadEd25519PrivateKeyPEM(filename)
	if err != nil || !privateKey.Equal(loaded) {
		t.Fatalf("0600 Ed25519 私钥应可加载: %v", err)
	}
}

func testArtifact(t *testing.T) ([]byte, Artifact) {
	t.Helper()
	packageBytes, _, err := PackageDirectory(writeTestPlugin(t))
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := Describe("stable", packageBytes)
	if err != nil {
		t.Fatal(err)
	}
	return packageBytes, artifact
}
