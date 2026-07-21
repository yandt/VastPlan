package catalog

import (
	"bytes"
	"crypto/ed25519"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/pluginservice"
)

func TestStoreRecoversCatalogAndKeepsMonotonicJournal(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repository")
	repository, privateKey := testSignedRepository(t, root)
	first, firstProof := publishTestArtifact(t, repository, privateKey, "1.0.0-dev.20260720.1.abcdef0")

	store, err := Open(root, repository)
	if err != nil {
		t.Fatal(err)
	}
	page := store.Query(Query{PluginPrefix: "com.example", Publisher: "example", Channel: "testing", Target: "backend", Page: 1, PageSize: 10})
	if page.Revision != 1 || page.Total != 1 || len(page.Items) != 1 || page.Items[0].SHA256 != first.SHA256 {
		t.Fatalf("恢复后的 Catalog 不完整: %#v", page)
	}
	if filtered := store.Query(Query{Lifecycle: LifecycleYanked, Page: 1, PageSize: 10}); filtered.Total != 0 {
		t.Fatalf("生命周期过滤必须使用当前状态: %#v", filtered)
	}
	journal := store.Journal(0, 10)
	if len(journal.Items) != 1 || !journal.Items[0].Recovered || journal.Items[0].Revision != 1 {
		t.Fatalf("首次发现的已有制品应形成恢复事件: %#v", journal)
	}
	if revision, err := store.RecordPublished(first, firstProof, time.Now().UTC()); err != nil || revision != 1 {
		t.Fatalf("相同制品重传必须幂等: revision=%d err=%v", revision, err)
	}

	second, secondProof := publishTestArtifact(t, repository, privateKey, "1.0.0-dev.20260720.2.bcdef01")
	if revision, err := store.RecordPublished(second, secondProof, time.Now().UTC()); err != nil || revision != 2 {
		t.Fatalf("第二个制品应取得单调 revision 2: revision=%d err=%v", revision, err)
	}
	after := store.Journal(1, 10)
	if after.Revision != 2 || len(after.Items) != 1 || after.Items[0].Revision != 2 || after.Items[0].Recovered {
		t.Fatalf("增量流水账不正确: %#v", after)
	}

	reopened, err := Open(root, repository)
	if err != nil {
		t.Fatalf("重启时应从事件和签名制品重建: %v", err)
	}
	if stats := reopened.Stats(); stats.Revision != 2 || stats.Artifacts != 2 {
		t.Fatalf("重启不得重复增加 revision: %#v", stats)
	}
	newest := reopened.Query(Query{PluginID: "com.example.catalog", Page: 1, PageSize: 1})
	if len(newest.Items) != 1 || newest.Items[0].Ref.Version != second.Version {
		t.Fatalf("同插件版本应按 SemVer 从新到旧分页: %#v", newest)
	}
}

func TestCatalogHTTPRequiresReadTokenTLSAndBoundedQuery(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repository")
	repository, privateKey := testSignedRepository(t, root)
	publishTestArtifact(t, repository, privateKey, "1.0.0-dev.20260720.1.abcdef0")
	store, err := Open(root, repository)
	if err != nil {
		t.Fatal(err)
	}
	handler := &HTTPHandler{Store: store, ReadToken: "reader", RequireTLS: true}

	request := httptest.NewRequest(http.MethodGet, "https://repository.test/v1/catalog/artifacts?channel=testing&pageSize=10", nil)
	request.TLS = &tls.ConnectionState{HandshakeComplete: true}
	request.Header.Set("Authorization", "Bearer reader")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("合法目录查询失败: status=%d body=%s", response.Code, response.Body.String())
	}
	var page Page
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil || page.Total != 1 {
		t.Fatalf("目录响应无效: page=%#v err=%v", page, err)
	}

	resolveBody := []byte(`{"roots":[{"pluginId":"com.example.catalog","constraint":"=1.0.0-dev.20260720.1.abcdef0"}],"target":"backend","kernelVersion":"0.1.0","allowedChannels":["testing"],"allowedPublishers":["example"],"allowedPluginPrefixes":["com.example"]}`)
	resolveRequest := httptest.NewRequest(http.MethodPost, "https://repository.test/v1/catalog/resolve", bytes.NewReader(resolveBody))
	resolveRequest.TLS = &tls.ConnectionState{HandshakeComplete: true}
	resolveRequest.Header.Set("Authorization", "Bearer reader")
	resolveResponse := httptest.NewRecorder()
	handler.ServeHTTP(resolveResponse, resolveRequest)
	if resolveResponse.Code != http.StatusOK {
		t.Fatalf("依赖解析失败: status=%d body=%s", resolveResponse.Code, resolveResponse.Body.String())
	}
	var lock pluginv1.ArtifactLock
	if err := json.Unmarshal(resolveResponse.Body.Bytes(), &lock); err != nil || len(lock.Packages) != 1 || lock.RepositoryRevision != 1 {
		t.Fatalf("解析锁无效: lock=%#v err=%v", lock, err)
	}

	for name, target := range map[string]struct {
		request *http.Request
		status  int
	}{
		"missing token": {httptest.NewRequest(http.MethodGet, "https://repository.test/v1/catalog/artifacts", nil), http.StatusUnauthorized},
		"plain HTTP":    {httptest.NewRequest(http.MethodGet, "http://repository.test/v1/catalog/artifacts", nil), http.StatusUpgradeRequired},
		"unknown query": {httptest.NewRequest(http.MethodGet, "https://repository.test/v1/catalog/artifacts?latest=true", nil), http.StatusBadRequest},
	} {
		t.Run(name, func(t *testing.T) {
			if name != "plain HTTP" {
				target.request.TLS = &tls.ConnectionState{HandshakeComplete: true}
			}
			if name == "unknown query" {
				target.request.Header.Set("Authorization", "Bearer reader")
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, target.request)
			if recorder.Code != target.status {
				t.Fatalf("status=%d want=%d body=%s", recorder.Code, target.status, recorder.Body.String())
			}
		})
	}
}

func TestLifecycleIsCASAuditedSnapshotAwareAndRestartSafe(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repository")
	repository, privateKey := testSignedRepository(t, root)
	artifact, _ := publishTestArtifact(t, repository, privateKey, "1.0.0-dev.20260721.1.abcdef0")
	store, err := Open(root, repository)
	if err != nil {
		t.Fatal(err)
	}
	ref := pluginv1.ArtifactRef{PluginID: artifact.PluginID, Version: artifact.Version, Channel: artifact.Channel}
	replacement := &pluginv1.ArtifactRequirement{PluginID: "com.example.catalog", Constraint: ">=2.0.0"}
	entry, revision, err := store.SetLifecycle(LifecycleRequest{Ref: ref, Status: LifecycleDeprecated, Reason: "use the maintained major", Replacement: replacement, ExpectedRevision: 1}, time.Now().UTC())
	if err != nil || revision != 2 || entry.LifecycleStatus != LifecycleDeprecated {
		t.Fatalf("deprecate failed: entry=%+v revision=%d err=%v", entry, revision, err)
	}
	request := pluginv1.ArtifactResolveRequest{Roots: []pluginv1.ArtifactRequirement{{PluginID: ref.PluginID, Constraint: "=" + ref.Version}}, Target: "backend", KernelVersion: "0.1.0", AllowedChannels: []string{"testing"}, AllowedPublishers: []string{"example"}, AllowedPluginPrefixes: []string{"com.example"}}
	lock, err := store.Resolve(request)
	if err != nil || lock.Packages[0].LifecycleStatus != LifecycleDeprecated || lock.Packages[0].Replacement == nil {
		t.Fatalf("deprecated artifact must remain resolvable with warning metadata: lock=%+v err=%v", lock, err)
	}
	request.SnapshotRevision = 1
	oldLock, err := store.Resolve(request)
	if err != nil || oldLock.Packages[0].LifecycleStatus != "" {
		t.Fatalf("older snapshot must keep its historical lifecycle view: lock=%+v err=%v", oldLock, err)
	}
	if _, actual, err := store.SetLifecycle(LifecycleRequest{Ref: ref, Status: LifecycleYanked, Reason: "bad release", ExpectedRevision: 1}, time.Now().UTC()); err == nil || actual != 2 {
		t.Fatalf("stale CAS must fail with current revision: actual=%d err=%v", actual, err)
	}
	if _, revision, err = store.SetLifecycle(LifecycleRequest{Ref: ref, Status: LifecycleYanked, Reason: "bad release", ExpectedRevision: 2}, time.Now().UTC()); err != nil || revision != 3 {
		t.Fatalf("yank failed: revision=%d err=%v", revision, err)
	}
	request.SnapshotRevision = 0
	if _, err := store.Resolve(request); err == nil {
		t.Fatal("yanked artifact must be excluded from new resolution")
	}
	reopened, err := Open(root, repository)
	if err != nil {
		t.Fatal(err)
	}
	page := reopened.Query(Query{PluginID: ref.PluginID, Page: 1, PageSize: 10})
	if page.Revision != 3 || len(page.Items) != 1 || page.Items[0].LifecycleStatus != LifecycleYanked {
		t.Fatalf("restart lost lifecycle state: %+v", page)
	}
}

func testSignedRepository(t *testing.T, root string) (*pluginservice.SignedRepository, ed25519.PrivateKey) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	trust, err := pluginservice.NewTrustStore(pluginservice.TrustDocumentForPublicKeys(pluginservice.TrustKey{
		Publisher: "example", KeyID: "testing", PublicKey: base64.StdEncoding.EncodeToString(publicKey),
	}))
	if err != nil {
		t.Fatal(err)
	}
	local, err := pluginservice.NewRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	return &pluginservice.SignedRepository{Local: local, Trust: trust}, privateKey
}

func publishTestArtifact(t *testing.T, repository *pluginservice.SignedRepository, privateKey ed25519.PrivateKey, version string) (pluginservice.Artifact, []byte) {
	t.Helper()
	directory := t.TempDir()
	manifest := []byte(`{
  "id":"com.example.catalog","name":"Catalog example","description":"Catalog example plugin","version":"` + version + `","publisher":"example",
  "engines":{"backend":"^0.1"},"activation":["onStartup"],"entry":{"backend":"backend/main"},
  "contributes":{"backend":{"tools":[{"id":"example.catalog","service_role":"backend","subcommands":[]}]}}
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
	packageBytes, parsed, err := pluginservice.PackageDirectory(directory)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := pluginservice.Describe("testing", packageBytes)
	if err != nil {
		t.Fatal(err)
	}
	attestation, err := pluginservice.SignArtifact(artifact, parsed.Publisher, "testing", privateKey, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Publish(attestation, packageBytes); err != nil {
		t.Fatal(err)
	}
	proof, err := json.Marshal(attestation)
	if err != nil {
		t.Fatal(err)
	}
	return artifact, proof
}
