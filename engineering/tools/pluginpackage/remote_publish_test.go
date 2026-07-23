package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"cdsoft.com.cn/VastPlan/core/kernels/backend/pluginservice"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifactapi"
)

func TestStableRemotePublishReusesExactVerifiedTestingCandidate(t *testing.T) {
	fixture := newRemotePublishFixture(t, "candidate-a")
	published, err := publishRemote(fixture.packageBytes, "vastplan", "stable", fixture.options)
	if err != nil {
		t.Fatal(err)
	}
	if published.Channel != "stable" || published.SHA256 != fixture.testing.SHA256 {
		t.Fatalf("stable 发布结果未绑定 testing 候选: %+v", published)
	}
	if _, _, err := fixture.repository.Read(pluginservice.Ref{PluginID: published.PluginID, Version: published.Version, Channel: "stable"}); err != nil {
		t.Fatalf("stable 制品未写入仓库: %v", err)
	}
}

func TestStableRemotePublishRejectsRebuiltDifferentBytes(t *testing.T) {
	fixture := newRemotePublishFixture(t, "candidate-a")
	different := packageForRemotePublish(t, "candidate-b")
	if _, err := publishRemote(different, "vastplan", "stable", fixture.options); err == nil {
		t.Fatal("与 testing 候选 SHA 不一致的 stable 包必须在上传前拒绝")
	}
	if _, _, err := fixture.repository.Read(pluginservice.Ref{PluginID: fixture.testing.PluginID, Version: fixture.testing.Version, Channel: "stable"}); err == nil {
		t.Fatal("预检失败不得产生 stable 制品")
	}
}

type remotePublishFixture struct {
	packageBytes []byte
	testing      pluginservice.Artifact
	repository   *pluginservice.SignedRepository
	options      remotePublishOptions
}

func newRemotePublishFixture(t *testing.T, payload string) remotePublishFixture {
	t.Helper()
	public, private, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	trustDocument := pluginservice.TrustDocumentForPublicKeys(pluginservice.TrustKey{Publisher: "vastplan", KeyID: "release", PublicKey: base64.StdEncoding.EncodeToString(public)})
	trust, err := pluginservice.NewTrustStore(trustDocument)
	if err != nil {
		t.Fatal(err)
	}
	local, err := pluginservice.NewRepository(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repository := &pluginservice.SignedRepository{Local: local, Trust: trust}
	packageBytes := packageForRemotePublish(t, payload)
	testingArtifact, err := pluginservice.Describe("testing", packageBytes)
	if err != nil {
		t.Fatal(err)
	}
	proof, err := pluginservice.SignArtifact(testingArtifact, "vastplan", "release", private, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Publish(proof, packageBytes); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewTLSServer(&artifactapi.Server{Repository: pluginservice.HTTPRepositoryAdapter{Repository: repository}, ReadToken: "reader", PublishToken: "publisher", RequireTLS: true})
	t.Cleanup(server.Close)
	trustRaw, _ := json.Marshal(trustDocument)
	trustFile := filepath.Join(t.TempDir(), "trust.json")
	if err := os.WriteFile(trustFile, trustRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	privateRaw, err := pluginservice.MarshalEd25519PrivateKeyPEM(private)
	if err != nil {
		t.Fatal(err)
	}
	privateFile := filepath.Join(t.TempDir(), "release.pem")
	if err := os.WriteFile(privateFile, privateRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	return remotePublishFixture{packageBytes: packageBytes, testing: testingArtifact, repository: repository, options: remotePublishOptions{RepositoryURL: server.URL, PublishToken: "publisher", ReadToken: "reader", TrustFile: trustFile, SignKey: privateFile, KeyID: "release", Timeout: time.Minute, Client: server.Client()}}
}

func packageForRemotePublish(t *testing.T, payload string) []byte {
	t.Helper()
	root := t.TempDir()
	manifest := `{"id":"cn.vastplan.product.release-test","name":"Release test","description":"Release test","version":"1.0.0","publisher":"vastplan","engines":{"backend":"^0.1"},"activation":["onStartup"],"entry":{"backend":"backend/main"},"contributes":{"backend":{"tools":[{"id":"product.release-test","service_role":"backend","subcommands":[]}]}}}`
	if err := os.WriteFile(filepath.Join(root, "vastplan.plugin.json"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "backend"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "backend", "main"), []byte(payload), 0o700); err != nil {
		t.Fatal(err)
	}
	packageBytes, _, err := pluginservice.PackageDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	return packageBytes
}
