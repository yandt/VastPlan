package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/bootstrapinventory"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/artifactrepository"
)

func TestSeedRuntimeSnapshotCommitsOnlyWhenExplicitlyPromotedAndRestores(t *testing.T) {
	root := platformDevTestProjectRoot(t)
	stateRoot := filepath.Join(t.TempDir(), "state-root")
	firstRun := filepath.Join(stateRoot, "runs", "first")
	r := &runtime{options: options{root: root, stateRoot: stateRoot}, runDir: firstRun}
	source := writeSeedRuntimeSnapshotTestSource(t, root, "first")

	if err := r.stageSeedRuntimeSnapshot(source, "test-build"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(r.seedRuntimeSnapshotPointerPath()); !os.IsNotExist(err) {
		t.Fatalf("候选快照不得在服务收敛前成为活动快照: %v", err)
	}
	if err := r.commitSeedRuntimeSnapshot(); err != nil {
		t.Fatal(err)
	}

	secondRun := filepath.Join(stateRoot, "runs", "second")
	if err := os.MkdirAll(secondRun, 0o700); err != nil {
		t.Fatal(err)
	}
	restoredRuntime := &runtime{options: options{root: root, stateRoot: stateRoot}, runDir: secondRun}
	refs, restored, err := restoredRuntime.restoreSeedRuntimeSnapshot()
	if err != nil || !restored || len(refs) != 1 {
		t.Fatalf("活动快照必须可恢复: refs=%v restored=%v err=%v", refs, restored, err)
	}
	for _, relative := range []string{"dynamic/backend-kernel", "dynamic/vastplan-go-dynamic-host", "portal-assets/index.html", "repository", "access-profile-catalog.json", "backend-platform-catalog.json"} {
		if _, err := os.Stat(filepath.Join(secondRun, filepath.FromSlash(relative))); err != nil {
			t.Fatalf("恢复内容缺失 %s: %v", relative, err)
		}
	}
	if _, err := os.Stat(filepath.Join(secondRun, "seed-inventory.json")); !os.IsNotExist(err) {
		t.Fatalf("每次启动必须重新生成签名绑定的 inventory，不得直接复制旧文件: %v", err)
	}
}

func TestBootstrapRestoresStableRuntimeWithoutOverwritingFreshCatalogs(t *testing.T) {
	root := platformDevTestProjectRoot(t)
	stateRoot := filepath.Join(t.TempDir(), "state-root")
	first := &runtime{options: options{root: root, stateRoot: stateRoot}, runDir: filepath.Join(stateRoot, "runs", "first")}
	if err := first.stageSeedRuntimeSnapshot(writeSeedRuntimeSnapshotTestSource(t, root, "stable"), "test-build"); err != nil {
		t.Fatal(err)
	}
	if err := first.commitSeedRuntimeSnapshot(); err != nil {
		t.Fatal(err)
	}

	secondRun := filepath.Join(stateRoot, "runs", "bootstrap")
	if err := os.MkdirAll(secondRun, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"access-profile-catalog.json", "backend-platform-catalog.json"} {
		if err := os.WriteFile(filepath.Join(secondRun, name), []byte("fresh-"+name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	bootstrap := &runtime{options: options{root: root, stateRoot: stateRoot, applyPlatform: true}, runDir: secondRun}
	if _, restored, err := bootstrap.restoreSeedRuntimeSnapshot(); err != nil || !restored {
		t.Fatalf("显式 bootstrap 应能复用 stable LKG: restored=%v err=%v", restored, err)
	}
	for _, name := range []string{"access-profile-catalog.json", "backend-platform-catalog.json"} {
		raw, err := os.ReadFile(filepath.Join(secondRun, name))
		if err != nil || string(raw) != "fresh-"+name {
			t.Fatalf("bootstrap 必须保留本次配置物化的 %s: %q err=%v", name, raw, err)
		}
	}
}

func TestSeedRuntimeSnapshotFailsClosedAfterTampering(t *testing.T) {
	root := platformDevTestProjectRoot(t)
	stateRoot := filepath.Join(t.TempDir(), "state-root")
	r := &runtime{options: options{root: root, stateRoot: stateRoot}, runDir: filepath.Join(stateRoot, "runs", "first")}
	if err := r.stageSeedRuntimeSnapshot(writeSeedRuntimeSnapshotTestSource(t, root, "first"), "test-build"); err != nil {
		t.Fatal(err)
	}
	if err := r.commitSeedRuntimeSnapshot(); err != nil {
		t.Fatal(err)
	}
	pointerRaw, err := os.ReadFile(r.seedRuntimeSnapshotPointerPath())
	if err != nil {
		t.Fatal(err)
	}
	var pointer seedRuntimeSnapshotPointer
	if err := json.Unmarshal(pointerRaw, &pointer); err != nil {
		t.Fatal(err)
	}
	tampered := filepath.Join(r.seedRuntimeSnapshotRoot(), pointer.Digest, "portal-assets", "index.html")
	if err := os.WriteFile(tampered, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, restored, err := (&runtime{options: r.options, runDir: filepath.Join(stateRoot, "runs", "second")}).restoreSeedRuntimeSnapshot()
	if err == nil || restored || !strings.Contains(err.Error(), "摘要不匹配") {
		t.Fatalf("被篡改的活动快照必须失败关闭: restored=%v err=%v", restored, err)
	}
}

func TestSeedRuntimeSnapshotDoesNotReplaceCorruptDigestDirectory(t *testing.T) {
	root := platformDevTestProjectRoot(t)
	stateRoot := filepath.Join(t.TempDir(), "state-root")
	r := &runtime{options: options{root: root, stateRoot: stateRoot}, runDir: filepath.Join(stateRoot, "runs", "first")}
	if err := r.stageSeedRuntimeSnapshot(writeSeedRuntimeSnapshotTestSource(t, root, "first"), "test-build"); err != nil {
		t.Fatal(err)
	}
	marker, err := readSeedRuntimeSnapshotMarker(r.seedSnapshotCandidate)
	if err != nil {
		t.Fatal(err)
	}
	conflict := filepath.Join(r.seedRuntimeSnapshotRoot(), marker.Digest)
	if err := os.MkdirAll(conflict, 0o700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(conflict, "sentinel")
	if err := os.WriteFile(sentinel, []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := r.commitSeedRuntimeSnapshot(); err == nil || !strings.Contains(err.Error(), "拒绝破坏性替换") {
		t.Fatalf("损坏的同摘要目录必须失败关闭: %v", err)
	}
	if raw, err := os.ReadFile(sentinel); err != nil || string(raw) != "preserve" {
		t.Fatalf("失败关闭不得删除既有目录: %q err=%v", raw, err)
	}
	if _, err := os.Stat(r.seedRuntimeSnapshotPointerPath()); !os.IsNotExist(err) {
		t.Fatalf("冲突时不得推进活动指针: %v", err)
	}
}

func TestSeedRuntimeSnapshotMigratesOnlyActiveHistoricalRun(t *testing.T) {
	root := platformDevTestProjectRoot(t)
	stateRoot := filepath.Join(t.TempDir(), "state-root")
	activeRun := filepath.Join(stateRoot, "runs", "20260725T120000.000000000Z")
	staleRun := filepath.Join(stateRoot, "runs", "20260726T120000.000000000Z")
	materializeSeedRuntimeTestSource(t, writeSeedRuntimeSnapshotTestSource(t, root, "active"), activeRun)
	materializeSeedRuntimeTestSource(t, writeSeedRuntimeSnapshotTestSource(t, root, "stale"), staleRun)
	if err := os.MkdirAll(filepath.Join(stateRoot, "state"), 0o700); err != nil {
		t.Fatal(err)
	}
	actual := []byte(`{"units":{"platform":{"plugins":[{"root":"` + filepath.ToSlash(filepath.Join(activeRun, "installed", "backend", "artifact")) + `"}]}}}`)
	if err := os.WriteFile(filepath.Join(stateRoot, "state", "actual-state.json"), actual, 0o600); err != nil {
		t.Fatal(err)
	}
	currentRun := filepath.Join(stateRoot, "runs", "current")
	if err := os.MkdirAll(currentRun, 0o700); err != nil {
		t.Fatal(err)
	}
	r := &runtime{options: options{root: root, stateRoot: stateRoot}, runDir: currentRun}
	refs, restored, err := r.restoreOrMigrateSeedRuntimeSnapshot()
	if err != nil || !restored || len(refs) != 1 {
		t.Fatalf("应迁移 actual-state 指向的历史运行: refs=%v restored=%v err=%v", refs, restored, err)
	}
	raw, err := os.ReadFile(filepath.Join(currentRun, "dynamic", "backend-kernel"))
	if err != nil || string(raw) != "active-backend-kernel" {
		t.Fatalf("不得按目录时间误选非活动运行: %q err=%v", raw, err)
	}
}

func writeSeedRuntimeSnapshotTestSource(t *testing.T, projectRoot, label string) string {
	t.Helper()
	root := t.TempDir()
	for path, content := range map[string]string{
		"dynamic/backend-kernel":                label + "-backend-kernel",
		"dynamic/vastplan-go-dynamic-host":      label + "-dynamic-host",
		"portal-assets/index.html":              "<html>" + label + "</html>",
		"portal-assets/assets/portal-kernel.js": "export const label = '" + label + "';",
	} {
		absolute := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(absolute), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(absolute, []byte(content), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	repositorySource := writeStableIdentityTestRepository(t, "1.0.0", label)
	if err := materializeMutableCachedDirectory(repositorySource, filepath.Join(root, "repository")); err != nil {
		t.Fatal(err)
	}
	repository, err := artifactrepository.NewRepository(filepath.Join(root, "repository"))
	if err != nil {
		t.Fatal(err)
	}
	refs, err := repository.ListRefs()
	if err != nil || len(refs) != 1 {
		t.Fatalf("测试仓库引用无效: refs=%v err=%v", refs, err)
	}
	artifact, err := repository.ReadMetadata(refs[0])
	if err != nil {
		t.Fatal(err)
	}
	item := bootstrapinventory.Item{Ref: refs[0], SHA256: artifact.SHA256}
	if err := writeAtomicOwnerJSON(filepath.Join(root, "seed-inventory.json"), bootstrapinventory.Inventory{
		Version: 1, Generation: 1, RepositoryID: "seed-test", Seed: []bootstrapinventory.Item{item}, LastKnownGood: []bootstrapinventory.Item{item},
	}); err != nil {
		t.Fatal(err)
	}
	managedProfile, err := backendcompositionv1.ParsePlatformProfileFile(filepath.Join(projectRoot, "engineering", "deploy", "managed-services-profile.json"))
	if err != nil {
		t.Fatal(err)
	}
	catalog := backendcompositionv1.BackendPlatformCatalog{
		Document: compositioncommonv1.Document{Version: 1, Revision: 1, ID: "snapshot-test"},
		Profiles: []backendcompositionv1.PlatformProfile{managedProfile},
		Bindings: []backendcompositionv1.BackendPlatformBinding{{
			TenantID: "local", DeploymentName: "managed-services",
			PlatformProfile: compositioncommonv1.Ref{ID: managedProfile.ID, Revision: managedProfile.Revision, Digest: managedProfile.Digest()},
		}},
	}
	if err := writeAtomicOwnerJSON(filepath.Join(root, "backend-platform-catalog.json"), catalog); err != nil {
		t.Fatal(err)
	}
	if err := copySnapshotFile(filepath.Join(projectRoot, "engineering", "deploy", "portal-access-profile-catalog.json"), filepath.Join(root, "access-profile-catalog.json")); err != nil {
		t.Fatal(err)
	}
	return root
}

func materializeSeedRuntimeTestSource(t *testing.T, source, target string) {
	t.Helper()
	if err := materializeMutableCachedDirectory(source, target); err != nil {
		t.Fatal(err)
	}
}

func platformDevTestProjectRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}
