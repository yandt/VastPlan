package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cdsoft.com.cn/VastPlan/extensions/libraries/go/artifactrepository"
)

func TestStablePackageIdentityLedgerReusesSameRefInsteadOfRebuilding(t *testing.T) {
	ledgerPath := filepath.Join(t.TempDir(), "identity", "stable.json")
	first := writeStableIdentityTestRepository(t, "1.0.0", "first")
	firstBytes := readStableIdentityPackage(t, first, "cn.vastplan.test.identity", "1.0.0")
	if err := reconcileStablePackageIdentities(first, ledgerPath); err != nil {
		t.Fatal(err)
	}
	if err := reconcileStablePackageIdentities(first, ledgerPath); err != nil {
		t.Fatalf("相同制品必须幂等: %v", err)
	}
	conflict := writeStableIdentityTestRepository(t, "1.0.0", "changed")
	if err := reconcileStablePackageIdentities(conflict, ledgerPath); err != nil {
		t.Fatalf("同一 stable ref 必须复用已登记对象: %v", err)
	}
	if got := readStableIdentityPackage(t, conflict, "cn.vastplan.test.identity", "1.0.0"); !bytes.Equal(got, firstBytes) {
		t.Fatal("未提升 SemVer 的工作区构建不得进入 stable 仓库")
	}
	upgraded := writeStableIdentityTestRepository(t, "1.0.1", "changed")
	if err := reconcileStablePackageIdentities(upgraded, ledgerPath); err != nil {
		t.Fatalf("提升 SemVer 后应记录新身份: %v", err)
	}
	raw, err := os.ReadFile(ledgerPath)
	if err != nil {
		t.Fatal(err)
	}
	var ledger stablePackageIdentityLedger
	if err := json.Unmarshal(raw, &ledger); err != nil {
		t.Fatal(err)
	}
	if len(ledger.Artifacts) != 2 {
		t.Fatalf("账本必须保留历史稳定版本: %#v", ledger.Artifacts)
	}
	info, err := os.Stat(ledgerPath)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("身份账本必须仅属主可写: info=%v err=%v", info, err)
	}
}

func TestHydrateRecordedStablePackagesSkipsSourcePackagingForKnownRef(t *testing.T) {
	ledgerPath := filepath.Join(t.TempDir(), "identity", "stable.json")
	first := writeStableIdentityTestRepository(t, "1.0.0", "first")
	firstBytes := readStableIdentityPackage(t, first, "cn.vastplan.test.identity", "1.0.0")
	if err := reconcileStablePackageIdentities(first, ledgerPath); err != nil {
		t.Fatal(err)
	}
	repositoryRoot := t.TempDir()
	ref := artifactrepository.Ref{PluginID: "cn.vastplan.test.identity", Version: "1.0.0", Channel: "stable"}
	hydrated, err := hydrateRecordedStablePackages(repositoryRoot, ledgerPath, []artifactrepository.Ref{ref})
	if err != nil {
		t.Fatal(err)
	}
	if !hydrated[ref.PluginID] {
		t.Fatal("已登记的普通 stable 精确引用必须在源码打包前装入仓库")
	}
	if got := readStableIdentityPackage(t, repositoryRoot, ref.PluginID, ref.Version); !bytes.Equal(got, firstBytes) {
		t.Fatal("预装仓库的 stable 包必须保持原始字节")
	}
}

func TestHydrateRecordedStablePackagesLeavesDynamicGoForVariantValidation(t *testing.T) {
	ledgerPath := filepath.Join(t.TempDir(), "identity", "stable.json")
	fingerprint := strings.Repeat("a", 64)
	first := writeStableIdentityDynamicRepository(t, fingerprint, "first")
	if err := reconcileStablePackageIdentities(first, ledgerPath); err != nil {
		t.Fatal(err)
	}
	ref := artifactrepository.Ref{PluginID: "cn.vastplan.test.identity", Version: "1.0.0", Channel: "stable"}
	hydrated, err := hydrateRecordedStablePackages(t.TempDir(), ledgerPath, []artifactrepository.Ref{ref})
	if err != nil {
		t.Fatal(err)
	}
	if hydrated[ref.PluginID] {
		t.Fatal("dynamic-go 必须先由当前 Host ABI 生成 variant，再走复用核验")
	}
}

func TestDevelopmentStableArchiveKeepsHistoricalExactRefsAvailable(t *testing.T) {
	root := t.TempDir()
	ledgerPath := filepath.Join(root, "identity", "stable.json")
	source := writeStableIdentityTestRepository(t, "1.0.0", "historical")
	if err := reconcileStablePackageIdentities(source, ledgerPath); err != nil {
		t.Fatal(err)
	}
	repositoryRoot := filepath.Join(root, "run", "repository")
	privateKeyPath := filepath.Join(root, "run", "secrets", "artifact-signing.pem")
	trustPath := filepath.Join(root, "run", "secrets", "seed-artifact-trust.json")
	trustKey, err := ensureSigningIdentity(privateKeyPath, "vastplan", "local-development")
	if err != nil {
		t.Fatal(err)
	}
	if err := writeTrustDocument(trustPath, trustKey); err != nil {
		t.Fatal(err)
	}
	count, unavailable, err := hydrateDevelopmentStableArchive(repositoryRoot, ledgerPath, privateKeyPath, trustPath)
	if err != nil || count != 1 || unavailable != 0 {
		t.Fatalf("历史 stable 制品装入失败: count=%d unavailable=%d err=%v", count, unavailable, err)
	}
	ref := artifactrepository.Ref{PluginID: "cn.vastplan.test.identity", Version: "1.0.0", Channel: "stable"}
	repository, err := artifactrepository.NewRepository(repositoryRoot)
	if err != nil {
		t.Fatal(err)
	}
	artifact, packageBytes, err := repository.Read(ref)
	if err != nil || artifact.SHA256 == "" || string(packageBytes) == "" {
		t.Fatalf("历史精确引用不可读: artifact=%+v err=%v", artifact, err)
	}
	cacheInfo, err := os.Stat(stablePackageCacheObject(stablePackageCacheRoot(ledgerPath), artifact.SHA256))
	if err != nil {
		t.Fatal(err)
	}
	objectInfo, err := os.Stat(filepath.Join(repositoryRoot, "artifacts", ref.PluginID, ref.Version, ref.Channel, artifact.Object))
	if err != nil || !os.SameFile(cacheInfo, objectInfo) {
		t.Fatalf("历史包体必须以硬链接复用对象缓存: cache=%v object=%v err=%v", cacheInfo, objectInfo, err)
	}
	count, unavailable, err = hydrateDevelopmentStableArchive(repositoryRoot, ledgerPath, privateKeyPath, trustPath)
	if err != nil || count != 0 || unavailable != 0 {
		t.Fatalf("重复装入必须幂等: count=%d unavailable=%d err=%v", count, unavailable, err)
	}
}

func TestDevelopmentStableArchiveSkipsUnavailableUnreferencedHistory(t *testing.T) {
	root := t.TempDir()
	ledgerPath := filepath.Join(root, "stable.json")
	missing := stablePackageIdentity{PluginID: "cn.vastplan.test.missing", Version: "1.0.0", Channel: "stable", SHA256: strings.Repeat("a", 64)}
	if err := writeStablePackageIdentityLedger(ledgerPath, stablePackageIdentityLedger{Schema: developmentStablePackageIdentitySchema, Artifacts: []stablePackageIdentity{missing}}); err != nil {
		t.Fatal(err)
	}
	privateKeyPath := filepath.Join(root, "secrets", "artifact-signing.pem")
	trustPath := filepath.Join(root, "secrets", "seed-artifact-trust.json")
	trustKey, err := ensureSigningIdentity(privateKeyPath, "vastplan", "local-development")
	if err != nil {
		t.Fatal(err)
	}
	if err := writeTrustDocument(trustPath, trustKey); err != nil {
		t.Fatal(err)
	}
	count, unavailable, err := hydrateDevelopmentStableArchive(filepath.Join(root, "repository"), ledgerPath, privateKeyPath, trustPath)
	if err != nil || count != 0 || unavailable != 1 {
		t.Fatalf("无关历史对象缺失不得阻断启动: count=%d unavailable=%d err=%v", count, unavailable, err)
	}
}

func TestDevelopmentStableArchiveSkipsUnreferencedHistoryFromOlderManifestSchema(t *testing.T) {
	root := t.TempDir()
	ledgerPath := filepath.Join(root, "stable.json")
	packageBytes := legacyStablePackage(t)
	digest := sha256.Sum256(packageBytes)
	identity := stablePackageIdentity{
		PluginID: "cn.vastplan.test.legacy", Version: "0.9.0", Channel: "stable", SHA256: hex.EncodeToString(digest[:]),
	}
	if err := writeStablePackageIdentityLedger(ledgerPath, stablePackageIdentityLedger{Schema: developmentStablePackageIdentitySchema, Artifacts: []stablePackageIdentity{identity}}); err != nil {
		t.Fatal(err)
	}
	cacheFile := stablePackageCacheObject(stablePackageCacheRoot(ledgerPath), identity.SHA256)
	if err := os.MkdirAll(filepath.Dir(cacheFile), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cacheFile, packageBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	privateKeyPath := filepath.Join(root, "secrets", "artifact-signing.pem")
	trustPath := filepath.Join(root, "secrets", "seed-artifact-trust.json")
	trustKey, err := ensureSigningIdentity(privateKeyPath, "vastplan", "local-development")
	if err != nil {
		t.Fatal(err)
	}
	if err := writeTrustDocument(trustPath, trustKey); err != nil {
		t.Fatal(err)
	}
	count, unavailable, err := hydrateDevelopmentStableArchive(filepath.Join(root, "repository"), ledgerPath, privateKeyPath, trustPath)
	if err != nil || count != 0 || unavailable != 1 {
		t.Fatalf("未引用的旧 Manifest Schema 历史对象不得阻断启动: count=%d unavailable=%d err=%v", count, unavailable, err)
	}
}

func legacyStablePackage(t *testing.T) []byte {
	t.Helper()
	manifest := []byte(`{
  "id":"cn.vastplan.test.legacy",
  "name":"Legacy stable fixture",
  "description":"legacy stable fixture",
  "version":"0.9.0",
  "publisher":"vastplan",
  "engines":{"frontend":"^1.0"},
  "activation":["onPortalStartup"],
  "entry":{"frontend":"frontend/dist/index.js"},
  "contributes":{"frontend":{"views":[],"menus":[]}}
}`)
	var out bytes.Buffer
	gz := gzip.NewWriter(&out)
	tw := tar.NewWriter(gz)
	for name, content := range map[string][]byte{
		"vastplan.plugin.json":   manifest,
		"frontend/dist/index.js": []byte(`export default {};`),
	} {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(content))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestStablePackageIdentityLedgerFailsClosedWhenCorrupted(t *testing.T) {
	root := t.TempDir()
	ledgerPath := filepath.Join(root, "stable.json")
	if err := os.WriteFile(ledgerPath, []byte(`{"schema":1,"artifacts":[{"pluginId":"cn.vastplan.test","version":"1.0.0","channel":"stable","sha256":"bad"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	err := reconcileStablePackageIdentities(writeStableIdentityTestRepository(t, "1.0.0", "first"), ledgerPath)
	if err == nil || !strings.Contains(err.Error(), "SHA-256 无效") {
		t.Fatalf("损坏账本不得被静默覆盖: %v", err)
	}
}

func TestStablePackageIdentityLedgerReusesAllRecordedRefs(t *testing.T) {
	ledgerPath := filepath.Join(t.TempDir(), "stable.json")
	first := writeStableIdentityMultiRepository(t, map[string]string{
		"cn.vastplan.test.alpha": "alpha-first",
		"cn.vastplan.test.beta":  "beta-first",
	})
	if err := reconcileStablePackageIdentities(first, ledgerPath); err != nil {
		t.Fatal(err)
	}
	alphaBytes := readStableIdentityPackage(t, first, "cn.vastplan.test.alpha", "1.0.0")
	betaBytes := readStableIdentityPackage(t, first, "cn.vastplan.test.beta", "1.0.0")
	changed := writeStableIdentityMultiRepository(t, map[string]string{
		"cn.vastplan.test.alpha": "alpha-changed",
		"cn.vastplan.test.beta":  "beta-changed",
	})
	if err := reconcileStablePackageIdentities(changed, ledgerPath); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(readStableIdentityPackage(t, changed, "cn.vastplan.test.alpha", "1.0.0"), alphaBytes) || !bytes.Equal(readStableIdentityPackage(t, changed, "cn.vastplan.test.beta", "1.0.0"), betaBytes) {
		t.Fatal("所有已登记 stable 精确引用都必须复用旧对象")
	}
}

func TestStablePackageIdentityLedgerScopesDynamicGoByBuildFingerprint(t *testing.T) {
	ledgerPath := filepath.Join(t.TempDir(), "stable.json")
	firstFingerprint := strings.Repeat("a", 64)
	secondFingerprint := strings.Repeat("b", 64)
	if err := reconcileStablePackageIdentities(writeStableIdentityDynamicRepository(t, firstFingerprint, "first"), ledgerPath); err != nil {
		t.Fatal(err)
	}
	secondOriginal := writeStableIdentityDynamicRepository(t, secondFingerprint, "second")
	if err := reconcileStablePackageIdentities(secondOriginal, ledgerPath); err != nil {
		t.Fatalf("不同 Backend 共同构建指纹是不同 dynamic-go variant: %v", err)
	}
	secondBytes := readStableIdentityPackage(t, secondOriginal, "cn.vastplan.test.identity", "1.0.0")
	second := writeStableIdentityDynamicRepository(t, secondFingerprint, "changed")
	if err := reconcileStablePackageIdentities(second, ledgerPath); err != nil {
		t.Fatalf("同一 dynamic-go variant 必须复用已登记对象: %v", err)
	}
	if got := readStableIdentityPackage(t, second, "cn.vastplan.test.identity", "1.0.0"); !bytes.Equal(got, secondBytes) {
		t.Fatal("同一 dynamic-go variant 的工作区变化不得覆盖 stable")
	}
}

func TestStablePackageIdentityLedgerFailsClosedWhenRecordedObjectIsMissing(t *testing.T) {
	root := t.TempDir()
	ledgerPath := filepath.Join(root, "identity", "stable.json")
	first := writeStableIdentityTestRepository(t, "1.0.0", "first")
	if err := reconcileStablePackageIdentities(first, ledgerPath); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(filepath.Dir(ledgerPath), "stable-packages")); err != nil {
		t.Fatal(err)
	}
	changed := writeStableIdentityTestRepository(t, "1.0.0", "changed")
	if err := reconcileStablePackageIdentities(changed, ledgerPath); err == nil || !strings.Contains(err.Error(), "缓存缺失") {
		t.Fatalf("已登记对象缺失时不得用当前源码覆盖: %v", err)
	}
}

func TestStablePackageIdentityLedgerFailsClosedWhenRecordedObjectIsCorrupted(t *testing.T) {
	root := t.TempDir()
	ledgerPath := filepath.Join(root, "identity", "stable.json")
	first := writeStableIdentityTestRepository(t, "1.0.0", "first")
	if err := reconcileStablePackageIdentities(first, ledgerPath); err != nil {
		t.Fatal(err)
	}
	ledger, err := loadStablePackageIdentityLedger(ledgerPath)
	if err != nil {
		t.Fatal(err)
	}
	cacheFile := stablePackageCacheObject(stablePackageCacheRoot(ledgerPath), ledger.Artifacts[0].SHA256)
	if err := os.WriteFile(cacheFile, []byte("corrupted"), 0o600); err != nil {
		t.Fatal(err)
	}
	changed := writeStableIdentityTestRepository(t, "1.0.0", "changed")
	if err := reconcileStablePackageIdentities(changed, ledgerPath); err == nil || !strings.Contains(err.Error(), "验证 stable 缓存对象") {
		t.Fatalf("缓存对象损坏时必须 fail closed: %v", err)
	}
}

func writeStableIdentityTestRepository(t *testing.T, version, content string) string {
	return writeStableIdentityRepository(t, version, content, "")
}

func writeStableIdentityDynamicRepository(t *testing.T, fingerprint, content string) string {
	return writeStableIdentityRepository(t, "1.0.0", content, fingerprint)
}

func writeStableIdentityRepository(t *testing.T, version, content, fingerprint string) string {
	t.Helper()
	repositoryRoot := t.TempDir()
	publishStableIdentityTestArtifact(t, repositoryRoot, "cn.vastplan.test.identity", version, content, fingerprint)
	return repositoryRoot
}

func writeStableIdentityMultiRepository(t *testing.T, artifacts map[string]string) string {
	t.Helper()
	repositoryRoot := t.TempDir()
	for pluginID, content := range artifacts {
		publishStableIdentityTestArtifact(t, repositoryRoot, pluginID, "1.0.0", content, "")
	}
	return repositoryRoot
}

func publishStableIdentityTestArtifact(t *testing.T, repositoryRoot, pluginID, version, content, fingerprint string) {
	t.Helper()
	pluginDir := t.TempDir()
	execution := ""
	if fingerprint != "" {
		execution = `,"execution":{"backend":{"driver":"native","minimumIsolation":"trusted-process","dynamicGo":{"entry":"backend/plugin.so","abi":"vastplan.dynamic-go.v1","fingerprint":"` + fingerprint + `","required":true}}}`
	}
	manifest := `{
  "id":"` + pluginID + `",
  "name":"Stable identity test",
  "description":"stable identity fixture",
  "version":"` + version + `",
  "publisher":"vastplan",
  "engines":{"backend":"^1.0"}` + execution + `,
  "activation":["onStartup"],
  "entry":{"backend":"backend/main"},
  "contributes":{"backend":{"tools":[{"id":"test.identity","service_role":"backend","title":"Identity fixture","subcommands":[{"name":"run","description":"run"}]}]}}
}`
	if err := os.WriteFile(filepath.Join(pluginDir, "vastplan.plugin.json"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(pluginDir, "backend"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "backend", "main"), []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
	if fingerprint != "" {
		if err := os.WriteFile(filepath.Join(pluginDir, "backend", "plugin.so"), []byte("so-"+content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	packageBytes, _, err := artifactrepository.PackageDirectory(pluginDir)
	if err != nil {
		t.Fatal(err)
	}
	repository, err := artifactrepository.NewRepository(repositoryRoot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Publish("stable", packageBytes); err != nil {
		t.Fatal(err)
	}
}

func readStableIdentityPackage(t *testing.T, repositoryRoot, pluginID, version string) []byte {
	t.Helper()
	repository, err := artifactrepository.NewRepository(repositoryRoot)
	if err != nil {
		t.Fatal(err)
	}
	_, packageBytes, err := repository.Read(artifactrepository.Ref{PluginID: pluginID, Version: version, Channel: "stable"})
	if err != nil {
		t.Fatal(err)
	}
	return packageBytes
}
