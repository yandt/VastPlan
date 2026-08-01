package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	artifactrepositoryv1 "cdsoft.com.cn/VastPlan/contracts/schemas/artifactrepository/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/artifactrepository"
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
	var document artifactrepository.TrustDocument
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Keys) != 2 || document.Keys[0].KeyID != "local-development" || document.Keys[1].KeyID != "local-testing" {
		t.Fatalf("组合信任快照必须同时包含 Seed 与测试身份: %#v", document.Keys)
	}
	if _, err := artifactrepository.LoadTrustStore(filename); err != nil {
		t.Fatalf("组合信任快照必须可由内核加载: %v", err)
	}
}

func TestManagedArtifactSourceUsesSeedBootstrapAndPersistentRepository(t *testing.T) {
	stateRoot := t.TempDir()
	runDir := filepath.Join(stateRoot, "runs", "current")
	r := runtime{
		runDir:  runDir,
		options: options{stateRoot: stateRoot, artifactListen: "127.0.0.1:18443", seedArtifactListen: "127.0.0.1:18442"},
		repositoryProfile: artifactrepositoryv1.Profile{
			Version: 1, ID: "local-testing", Protocol: artifactrepositoryv1.ProtocolLocalTest,
			Endpoint: "unix://" + filepath.Join(stateRoot, "repositories", "testing", "repository.sock"), Channels: []string{"testing"}, DevelopmentOnly: true,
		},
	}
	wantArgs := []string{
		"-bootstrap-repository", filepath.Join(runDir, "repository"),
		"-bootstrap-inventory", filepath.Join(runDir, "seed-inventory.json"),
		"-repository-profile", filepath.Join(runDir, "repository-profile.json"),
		"-repository-token-file", filepath.Join(runDir, "secrets", "artifact-local-test.token"),
		"-repository-trust", filepath.Join(runDir, "secrets", "artifact-trust.json"),
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
	if environment["VASTPLAN_DATABASE_CONNECTIONS_STATE_FILE"] != filepath.Join(wantStateRoot, "database-connections.json") {
		t.Fatalf("有永久引用或治理事实的插件状态必须跨普通重启保留: %#v", environment)
	}
	if environment["VASTPLAN_AUTHORIZATION_POLICY_STATE"] != environment["VASTPLAN_AUTHORIZATION_POLICY_BOOTSTRAP_STATE"] {
		t.Fatalf("Seed Runtime Snapshot v1 必须同时支持旧 Policy Store 与新 Bootstrap State 宿主契约: %#v", environment)
	}
	if _, enabled := environment["VASTPLAN_AUTHORIZATION_POLICY_BOOTSTRAP_RECONCILIATION"]; enabled {
		t.Fatal("普通 up/restart 不得启用 Bootstrap 授权协调")
	}
	r.options.applyPlatform = true
	if got := r.serviceEnv()["VASTPLAN_AUTHORIZATION_POLICY_BOOTSTRAP_RECONCILIATION"]; got != "seed-owned" {
		t.Fatalf("只有显式 bootstrap 可选择 Seed-owned 协调策略: %q", got)
	}
	if r.persistentStateRoot() != wantStateRoot {
		t.Fatalf("Node ActualState、Portal 交付快照与治理插件必须共享同一持久开发状态根: %s", r.persistentStateRoot())
	}
	wantCredentialRoot := filepath.Join(wantStateRoot, "node-bootstrap-credentials")
	if got := r.nodeBootstrapCredentialArgs(); !reflect.DeepEqual(got, []string{"-credential-root", wantCredentialRoot}) {
		t.Fatalf("开发 Node Agent 必须注册 fail-closed 的 Node Bootstrap Broker: got=%#v", got)
	}
	wantVolumeRoot := filepath.Join(stateRoot, "repositories", "testing", "volumes")
	if environment["VASTPLAN_ARTIFACT_FILE_PROVIDER_ROOT"] != wantVolumeRoot {
		t.Fatalf("File Provider 必须使用持久化测试目录: %#v", environment)
	}
	if environment["VASTPLAN_ARTIFACT_REPOSITORY"] != filepath.Join(wantVolumeRoot, "repository.primary") {
		t.Fatalf("托管仓库必须使用持久化测试 volume: %#v", environment)
	}
	if environment["VASTPLAN_ARTIFACT_ASSESSMENT_REPORTS"] != filepath.Join(stateRoot, "repositories", "testing", "assessment-reports") {
		t.Fatalf("安全评估报告必须使用独立共享归档: %#v", environment)
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
