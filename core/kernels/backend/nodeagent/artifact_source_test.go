package nodeagent

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"cdsoft.com.cn/VastPlan/core/kernels/backend/pluginservice"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifacttrust"
)

type staticArtifactSource struct {
	name  string
	env   artifacttrust.Envelope
	err   error
	calls int
}

func (s *staticArtifactSource) SourceName() string { return s.name }
func (s *staticArtifactSource) Fetch(context.Context, pluginv1.ArtifactRef) (artifacttrust.Envelope, error) {
	s.calls++
	return s.env, s.err
}

func TestArtifactVerifierRequiresProofAndProducesInstallerToken(t *testing.T) {
	packageBytes, artifact := testPackage(t, 0o755)
	ref := pluginv1.ArtifactRef{PluginID: artifact.PluginID, Version: artifact.Version, Channel: artifact.Channel}
	publicKey, privateKey, _ := ed25519.GenerateKey(nil)
	trust, err := pluginservice.NewTrustStore(pluginservice.TrustDocumentForPublicKeys(pluginservice.TrustKey{
		Publisher: "example", KeyID: "release", PublicKey: base64.StdEncoding.EncodeToString(publicKey),
	}))
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := NewSignedArtifactVerifier(trust)
	if err != nil {
		t.Fatal(err)
	}
	envelope := artifacttrust.Envelope{Artifact: artifact, PackageBytes: packageBytes}
	if _, err := verifier.Verify(ref, envelope); err == nil || !strings.Contains(err.Error(), "缺少发布者证明") {
		t.Fatalf("签名模式必须拒绝无证明来源: %v", err)
	}
	attestation, err := pluginservice.SignArtifact(artifact, "example", "release", privateKey, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	envelope.Proof, _ = json.Marshal(attestation)
	verified, err := verifier.Verify(ref, envelope)
	if err != nil {
		t.Fatal(err)
	}
	if verified.Artifact().SHA256 != artifact.SHA256 || string(verified.PackageBytes()) != string(packageBytes) {
		t.Fatal("验证结果没有冻结签名制品")
	}
	if _, err := (LocalInstaller{Root: t.TempDir()}).Install(VerifiedArtifact{}); err == nil {
		t.Fatal("安装器必须拒绝未由内核验证器构造的零值")
	}
}

func TestArtifactResolutionFallsBackOnlyOnNotFound(t *testing.T) {
	packageBytes, artifact := testPackage(t, 0o755)
	ref := pluginv1.ArtifactRef{PluginID: artifact.PluginID, Version: artifact.Version, Channel: artifact.Channel}
	valid := artifacttrust.Envelope{Artifact: artifact, PackageBytes: packageBytes}

	seed := &staticArtifactSource{name: "seed", err: artifacttrust.ErrNotFound}
	remote := &staticArtifactSource{name: "remote", env: valid}
	r := &Reconciler{
		Sources: []ArtifactSource{seed, remote}, Verifier: NewLocalDevelopmentArtifactVerifier(),
	}
	if _, err := r.resolveArtifact(context.Background(), ref); err != nil {
		t.Fatalf("种子精确缺失时应读取远端源: %v", err)
	}
	if seed.calls != 1 || remote.calls != 1 {
		t.Fatalf("来源调用顺序错误: seed=%d remote=%d", seed.calls, remote.calls)
	}

	tampered := valid
	tampered.PackageBytes = append([]byte(nil), packageBytes...)
	tampered.PackageBytes[len(tampered.PackageBytes)-1] ^= 0xff
	seed = &staticArtifactSource{name: "seed", env: tampered}
	remote = &staticArtifactSource{name: "remote", env: valid}
	r.Sources = []ArtifactSource{seed, remote}
	if _, err := r.resolveArtifact(context.Background(), ref); err == nil || !strings.Contains(err.Error(), "不可信内容") {
		t.Fatalf("种子返回篡改内容必须 fail-closed: %v", err)
	}
	if remote.calls != 0 {
		t.Fatal("验证失败不得静默换到远端源")
	}
}

func TestArtifactResolutionDoesNotTreatTransportFailureAsNotFound(t *testing.T) {
	first := &staticArtifactSource{name: "seed", err: errors.New("permission denied")}
	second := &staticArtifactSource{name: "remote"}
	r := &Reconciler{Sources: []ArtifactSource{first, second}, Verifier: NewLocalDevelopmentArtifactVerifier()}
	_, err := r.resolveArtifact(context.Background(), pluginv1.ArtifactRef{PluginID: "com.example.demo", Version: "1.0.0", Channel: "stable"})
	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("来源传输/权限失败必须原样暴露: %v", err)
	}
	if second.calls != 0 {
		t.Fatal("非 not found 错误不得换源")
	}
}
