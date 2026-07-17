package nodeagent

import (
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"cdsoft.com.cn/VastPlan/kernels/backend/pluginservice"
	deploymentv1 "cdsoft.com.cn/VastPlan/schemas/deployment/v1"
)

func TestReconciler_RemoteSignedArtifactToInstalledRuntime(t *testing.T) {
	packageBytes, artifact := testPackage(t, 0o755)
	publicKey, privateKey, _ := ed25519.GenerateKey(nil)
	trust, err := pluginservice.NewTrustStore(pluginservice.TrustDocumentForPublicKeys(pluginservice.TrustKey{
		Publisher: "example", KeyID: "release", PublicKey: base64.StdEncoding.EncodeToString(publicKey),
	}))
	if err != nil {
		t.Fatal(err)
	}
	attestation, err := pluginservice.SignArtifact(artifact, "example", "release", privateKey, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	local, _ := pluginservice.NewRepository(filepath.Join(t.TempDir(), "repository"))
	signed := &pluginservice.SignedRepository{Local: local, Trust: trust}
	if _, err := signed.Publish(attestation, packageBytes); err != nil {
		t.Fatal(err)
	}
	handler := &pluginservice.ArtifactHTTPServer{
		Repository: signed, ReadToken: "node-token", PublishToken: "publisher-token", RequireTLS: true,
	}
	remote := &pluginservice.RemoteRepository{
		BaseURL: "https://artifacts.example.test", Token: "node-token", Trust: trust,
		Client: &http.Client{Transport: nodeHandlerTransport{handler: handler}},
	}
	verifier, err := NewSignedArtifactVerifier(trust)
	if err != nil {
		t.Fatal(err)
	}
	runtime := newFakeRuntime()
	reconciler := &Reconciler{
		NodeID: "node-remote", Sources: []ArtifactSource{remote}, Verifier: verifier,
		Installer: LocalInstaller{Root: filepath.Join(t.TempDir(), "installed")},
		Runtime:   runtime, StateStore: NewMemoryStateStore(),
	}
	desiredState := deploymentv1.DesiredState{
		Version: 1, Revision: 1, Metadata: deploymentv1.Metadata{Name: "remote-artifact"},
		Units: []deploymentv1.Unit{{
			ID: "backend-main", Kind: "service", Enabled: true, ServiceRole: "backend", Replicas: 1,
			Plugins: []deploymentv1.PluginRef{{ID: artifact.PluginID, Version: artifact.Version, Channel: artifact.Channel}},
		}},
	}
	result, err := reconciler.Reconcile(context.Background(), desiredState)
	if err != nil || !result.Converged || !runtime.IsRunning("backend-main", desiredState.Units[0].Fingerprint()) {
		t.Fatalf("远端签名制品应完成下载、安装和运行时装配: result=%+v err=%v", result, err)
	}
}

type nodeHandlerTransport struct{ handler http.Handler }

func (t nodeHandlerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.TLS = &tls.ConnectionState{HandshakeComplete: true}
	clone.RemoteAddr = "127.0.0.1:12345"
	recorder := httptest.NewRecorder()
	t.handler.ServeHTTP(recorder, clone)
	return recorder.Result(), nil
}
