package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"cdsoft.com.cn/VastPlan/core/kernels/backend/pluginservice"
)

func TestEnsureSigningIdentityReusesPersistentPrivateKey(t *testing.T) {
	privateFile := filepath.Join(t.TempDir(), "secrets", "artifact-signing.pem")
	first, err := ensureSigningIdentity(privateFile, "vastplan", "local-testing")
	if err != nil {
		t.Fatal(err)
	}
	firstRaw, err := os.ReadFile(privateFile)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ensureSigningIdentity(privateFile, "vastplan", "local-testing")
	if err != nil {
		t.Fatal(err)
	}
	secondRaw, err := os.ReadFile(privateFile)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) || !reflect.DeepEqual(firstRaw, secondRaw) {
		t.Fatal("持久化测试签名身份不得在重启时变化")
	}
	info, err := os.Stat(privateFile)
	if err != nil || info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("测试签名私钥必须仅属主可访问: info=%v err=%v", info, err)
	}
}

func TestWriteTrustDocumentCombinesSeedAndTestingIdentities(t *testing.T) {
	root := t.TempDir()
	seed, err := ensureSigningIdentity(filepath.Join(root, "seed", "key.pem"), "vastplan", "local-development")
	if err != nil {
		t.Fatal(err)
	}
	testing, err := ensureSigningIdentity(filepath.Join(root, "testing", "key.pem"), "vastplan", "local-testing")
	if err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(root, "artifact-trust.json")
	if err := writeTrustDocument(filename, seed, testing); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	var document pluginservice.TrustDocument
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Keys) != 2 || document.Keys[0].KeyID != "local-development" || document.Keys[1].KeyID != "local-testing" {
		t.Fatalf("组合信任快照必须同时包含 Seed 与测试身份: %#v", document.Keys)
	}
	if _, err := pluginservice.LoadTrustStore(filename); err != nil {
		t.Fatalf("组合信任快照必须可由内核加载: %v", err)
	}
}

func TestManagedArtifactSourceUsesSeedBootstrapAndPersistentRepository(t *testing.T) {
	stateRoot := t.TempDir()
	runDir := filepath.Join(stateRoot, "runs", "current")
	r := runtime{
		runDir:  runDir,
		options: options{stateRoot: stateRoot, artifactListen: "127.0.0.1:18443", seedArtifactListen: "127.0.0.1:18442"},
	}
	wantArgs := []string{
		"-bootstrap-repository", filepath.Join(runDir, "repository"),
		"-bootstrap-inventory", filepath.Join(runDir, "seed-inventory.json"),
		"-repository-url", "https://127.0.0.1:18443",
		"-repository-trust", filepath.Join(runDir, "secrets", "artifact-trust.json"),
		"-repository-ca", filepath.Join(runDir, "secrets", "tls-cert.pem"),
	}
	if got := r.managedArtifactSourceArgs(); !reflect.DeepEqual(got, wantArgs) {
		t.Fatalf("Node Agent 制品源错误:\n got=%#v\nwant=%#v", got, wantArgs)
	}
	wantControllerArgs := wantArgs[4:]
	if got := r.controllerArtifactSourceArgs(); !reflect.DeepEqual(got, wantControllerArgs) {
		t.Fatalf("Controller 必须使用同一托管测试仓库后备源:\n got=%#v\nwant=%#v", got, wantControllerArgs)
	}
	environment := r.serviceEnv()
	wantStateRoot := filepath.Join(stateRoot, "state")
	if environment["VASTPLAN_CREDENTIALS_STATE_FILE"] != filepath.Join(wantStateRoot, "credentials.json") ||
		environment["VASTPLAN_DATABASE_CONNECTIONS_STATE_FILE"] != filepath.Join(wantStateRoot, "database-connections.json") ||
		environment["VASTPLAN_DEPLOYMENT_MANAGER_STATE_FILE"] != filepath.Join(wantStateRoot, "deployment-manager.json") {
		t.Fatalf("有永久引用或治理事实的插件状态必须跨普通重启保留: %#v", environment)
	}
	if r.persistentStateRoot() != wantStateRoot {
		t.Fatalf("Node ActualState、Portal 交付快照与治理插件必须共享同一持久开发状态根: %s", r.persistentStateRoot())
	}
	wantVolumeRoot := filepath.Join(stateRoot, "repositories", "testing", "volumes")
	if environment["VASTPLAN_ARTIFACT_FILE_PROVIDER_ROOT"] != wantVolumeRoot {
		t.Fatalf("File Provider 必须使用持久化测试目录: %#v", environment)
	}
	if environment["VASTPLAN_ARTIFACT_REPOSITORY"] != filepath.Join(wantVolumeRoot, "repository.primary") {
		t.Fatalf("托管仓库必须使用持久化测试 volume: %#v", environment)
	}
	if environment["VASTPLAN_ARTIFACT_TRUST"] != filepath.Join(stateRoot, "repositories", "testing", "artifact-trust.json") {
		t.Fatalf("托管仓库只能信任稳定测试发布身份: %#v", environment)
	}
	if _, exposed := environment["VASTPLAN_ARTIFACT_SIGNING_KEY"]; exposed {
		t.Fatal("测试签名私钥不得注入托管仓库插件")
	}
}

func TestEnsurePrivateDirectoryRejectsBroadPermissions(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "repository")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ensurePrivateDirectory(directory); err == nil {
		t.Fatal("持久化仓库目录权限过宽时必须 fail-closed")
	}
}
