package main

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cdsoft.com.cn/VastPlan/core/kernels/backend/pluginservice"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifactapi"
	"cdsoft.com.cn/VastPlan/core/shared/go/platformadminapi"
	artifactcatalog "cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.artifacts.repository/catalog"
)

type catalogingTestRepository struct {
	upstream artifactapi.Repository
	store    *artifactcatalog.Store
}

func (r catalogingTestRepository) Publish(attestationRaw, packageBytes []byte) (pluginservice.Artifact, error) {
	artifact, err := r.upstream.Publish(attestationRaw, packageBytes)
	if err != nil {
		return pluginservice.Artifact{}, err
	}
	_, err = r.store.RecordPublished(artifact, attestationRaw, time.Now().UTC())
	return artifact, err
}

func (r catalogingTestRepository) Read(ref pluginservice.Ref) (pluginservice.Artifact, []byte, []byte, error) {
	return r.upstream.Read(ref)
}

func TestPublishUsesOnlyManagedLocalTestingIdentity(t *testing.T) {
	stateRoot := t.TempDir()
	runDir := filepath.Join(stateRoot, "runs", "active")
	if err := os.MkdirAll(filepath.Join(runDir, "secrets"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "secrets", "artifact-publish.token"), []byte("publisher\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "secrets", "artifact-read.token"), []byte("reader\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	publicKey, privateKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	trustDocument := pluginservice.TrustDocumentForPublicKeys(pluginservice.TrustKey{
		Publisher: "vastplan", KeyID: "local-testing", PublicKey: base64.StdEncoding.EncodeToString(publicKey),
	})
	trustRaw, _ := json.Marshal(trustDocument)
	testingRoot := filepath.Join(stateRoot, "repositories", "testing")
	if err := os.MkdirAll(filepath.Join(testingRoot, "secrets"), 0o700); err != nil {
		t.Fatal(err)
	}
	trustFile := filepath.Join(testingRoot, "artifact-trust.json")
	if err := os.WriteFile(trustFile, trustRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	privateRaw, _ := pluginservice.MarshalEd25519PrivateKeyPEM(privateKey)
	if err := os.WriteFile(filepath.Join(testingRoot, "secrets", "artifact-signing.pem"), privateRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	trust, err := pluginservice.LoadTrustStore(trustFile)
	if err != nil {
		t.Fatal(err)
	}
	local, _ := pluginservice.NewRepository(filepath.Join(testingRoot, "repository"))
	signed := &pluginservice.SignedRepository{Local: local, Trust: trust}
	catalogStore, err := artifactcatalog.Open(filepath.Join(testingRoot, "repository"), signed)
	if err != nil {
		t.Fatal(err)
	}
	adapter := pluginservice.HTTPRepositoryAdapter{Repository: signed}
	artifactHandler := &artifactapi.Server{
		Repository: catalogingTestRepository{upstream: adapter, store: catalogStore},
		ReadToken:  "reader", PublishToken: "publisher", RequireTLS: true,
	}
	mux := http.NewServeMux()
	mux.Handle("/v1/catalog/", &artifactcatalog.HTTPHandler{Store: catalogStore, ReadToken: "reader", RequireTLS: true})
	mux.Handle("/", artifactHandler)
	artifactServer := httptest.NewTLSServer(mux)
	defer artifactServer.Close()
	certificate := artifactServer.Certificate()
	certificatePEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Raw})
	if err := os.WriteFile(filepath.Join(runDir, "secrets", "tls-cert.pem"), certificatePEM, 0o600); err != nil {
		t.Fatal(err)
	}

	statusServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ready": true, "mode": "local-development", "productionEquivalent": false, "runDir": runDir,
			"repositories": map[string]any{"testing": map[string]any{"url": artifactServer.URL, "persistent": true}},
		})
	}))
	defer statusServer.Close()
	packageFile := writeTestPackage(t, "0.4.0-dev.20260720.1.abcdef0")
	opts := options{
		PackageFile: packageFile, StateRoot: stateRoot, StatusURL: statusServer.URL, Timeout: 10 * time.Second,
	}
	if err := publish(context.Background(), opts); err != nil {
		t.Fatalf("受控本地测试发布失败: %v", err)
	}
	if err := publish(context.Background(), opts); err != nil {
		t.Fatalf("相同测试制品重试必须按原证明和 revision 幂等: %v", err)
	}
	if stats := catalogStore.Stats(); stats.Revision != 1 || stats.Artifacts != 1 {
		t.Fatalf("幂等重试不得增加流水账 revision: %#v", stats)
	}
	if _, _, err := (&pluginservice.SignedRepository{Local: local, Trust: trust}).Read(pluginservice.Ref{
		PluginID: "cn.vastplan.product.test.publish", Version: "0.4.0-dev.20260720.1.abcdef0", Channel: "testing",
	}); err != nil {
		t.Fatalf("仓库没有收到 testing 制品: %v", err)
	}
}

func TestTestingPublisherRejectsStableAndNonLoopbackTargets(t *testing.T) {
	if _, _, _, err := loadTestingArtifact(writeTestPackage(t, "0.4.0")); err == nil || !strings.Contains(err.Error(), "dev.*") {
		t.Fatalf("稳定版本不得通过测试上传器: %v", err)
	}
	for _, raw := range []string{"https://example.com", "http://127.0.0.1:18443"} {
		if _, err := loopbackURL(raw, true); err == nil {
			t.Fatalf("测试仓库地址必须是回环 HTTPS: %s", raw)
		}
	}
}

func TestConfinedRunDirRejectsPathOutsideStateRoot(t *testing.T) {
	stateRoot := t.TempDir()
	runDir := filepath.Join(stateRoot, "runs", "active")
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	resolvedRoot, _ := filepath.EvalSymlinks(stateRoot)
	if got, err := confinedRunDir(resolvedRoot, runDir); err != nil || got == "" {
		t.Fatalf("合法受管运行目录被拒绝: %s %v", got, err)
	}
	if _, err := confinedRunDir(resolvedRoot, t.TempDir()); err == nil {
		t.Fatal("状态端点不得把秘密读取重定向到状态根之外")
	}
}

func TestSubmitBackendTestReleaseCreatesBindingAndPublishesExactReceipt(t *testing.T) {
	var bindingRequest platformadminapi.PutTestTargetBindingRequest
	var releaseRequest platformadminapi.CreateTestReleaseRequest
	portal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		session, _ := request.Cookie("vastplan_session")
		switch {
		case request.URL.Path == "/v1/csrf":
			if session == nil || (session.Value != developmentAdminSession && session.Value != developmentPublisherSession) {
				http.Error(w, "missing development session", http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"token": "csrf-safe"})
		case request.Method == http.MethodGet && strings.HasSuffix(request.URL.Path, "/test-target-bindings"):
			if session == nil || session.Value != developmentAdminSession {
				http.Error(w, "wrong read session", http.StatusForbidden)
				return
			}
			_ = json.NewEncoder(w).Encode([]platformadminapi.TestTargetBinding{})
		case request.Method == http.MethodPut && strings.Contains(request.URL.Path, "/test-target-bindings/"):
			if session == nil || session.Value != developmentAdminSession || request.Header.Get("X-VastPlan-CSRF") != "csrf-safe" {
				http.Error(w, "wrong binding authorization", http.StatusForbidden)
				return
			}
			if err := json.NewDecoder(request.Body).Decode(&bindingRequest); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			id := request.URL.Path[strings.LastIndex(request.URL.Path, "/")+1:]
			_ = json.NewEncoder(w).Encode(platformadminapi.TestTargetBinding{
				ID: id, Kind: bindingRequest.Kind, Deployment: bindingRequest.Deployment, UnitID: bindingRequest.UnitID,
				PluginID: bindingRequest.PluginID, AllowedPublishers: bindingRequest.AllowedPublishers, Enabled: bindingRequest.Enabled, Version: 1,
			})
		case request.Method == http.MethodPost && strings.HasSuffix(request.URL.Path, "/test-releases"):
			if session == nil || session.Value != developmentPublisherSession || request.Header.Get("X-VastPlan-CSRF") != "csrf-safe" {
				http.Error(w, "wrong release authorization", http.StatusForbidden)
				return
			}
			if err := json.NewDecoder(request.Body).Decode(&releaseRequest); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(platformadminapi.TestRelease{
				ID: 3, BindingID: releaseRequest.BindingID, Artifact: releaseRequest.Artifact, SHA256: releaseRequest.SHA256,
				RepositoryRevision: releaseRequest.RepositoryRevision, Status: platformadminapi.TestReleaseReady, CandidateServiceRevisionID: 8,
			})
		default:
			http.NotFound(w, request)
		}
	}))
	defer portal.Close()
	artifact := pluginservice.Artifact{
		PluginID: "cn.vastplan.product.test.publish", Version: "0.4.0-dev.20260721.1.abcdef0", Channel: "testing", SHA256: strings.Repeat("a", 64),
	}
	status := developmentStatus{Portal: portal.URL + "/operations"}
	opts := options{BackendTarget: "managed-services/hello-service", Timeout: 5 * time.Second}
	if err := submitBackendTestRelease(context.Background(), status, opts, artifact, 17); err != nil {
		t.Fatalf("Backend 测试发布入口失败: %v", err)
	}
	if bindingRequest.Deployment != "managed-services" || bindingRequest.UnitID != "hello-service" || bindingRequest.PluginID != artifact.PluginID || !bindingRequest.Enabled {
		t.Fatalf("自动目标绑定错误: %+v", bindingRequest)
	}
	if releaseRequest.RepositoryRevision != 17 || releaseRequest.SHA256 != artifact.SHA256 || releaseRequest.Artifact.PluginID != artifact.PluginID || releaseRequest.BindingID == "" {
		t.Fatalf("Test Release 未使用精确仓库回执: %+v", releaseRequest)
	}
}

func writeTestPackage(t *testing.T, version string) string {
	t.Helper()
	directory := t.TempDir()
	manifest := []byte(`{
  "id":"cn.vastplan.product.test.publish","name":"Test publish","description":"Test publish plugin","version":"` + version + `","publisher":"vastplan",
  "engines":{"backend":"^0.1"},"activation":["onStartup"],"entry":{"backend":"backend/main"},
  "contributes":{"backend":{"tools":[{"id":"product.test.publish","service_role":"backend","subcommands":[]}]}}
}`)
	if err := os.WriteFile(filepath.Join(directory, "vastplan.plugin.json"), manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(directory, "backend"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "backend", "main"), []byte("binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	packageBytes, _, err := pluginservice.PackageDirectory(directory)
	if err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(t.TempDir(), "plugin.tar.gz")
	if err := os.WriteFile(filename, packageBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	return filename
}
