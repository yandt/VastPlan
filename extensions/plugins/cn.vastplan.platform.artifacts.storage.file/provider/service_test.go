package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifactstorage"
)

func TestProvisionIsIdempotentPrivateAndContained(t *testing.T) {
	root := filepath.Join(t.TempDir(), "provider")
	service, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	first, err := service.Provision("repository.primary")
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Provision("repository.primary")
	if err != nil || first.Handle != second.Handle || first.MountPath != second.MountPath {
		t.Fatalf("重复 provision 必须幂等: first=%+v second=%+v err=%v", first, second, err)
	}
	info, err := os.Stat(first.MountPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 || filepath.Dir(first.MountPath) != root {
		t.Fatalf("volume 必须私有且位于 provider root: path=%s mode=%v", first.MountPath, info.Mode())
	}
	if _, err := service.Provision("../escape"); err == nil {
		t.Fatal("目录逃逸 volume id 必须拒绝")
	}
}

func TestRuntimeDescriptorMatchesSignedManifest(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "vastplan.plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := pluginv1.ParseManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := pluginv1.BackendRuntimeContributions(manifest)
	if err != nil || len(expected) != 1 {
		t.Fatalf("manifest contributions invalid: %+v err=%v", expected, err)
	}
	root := filepath.Join(t.TempDir(), "provider")
	service, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	actual := service.Contribution()
	var left, right any
	if json.Unmarshal(expected[0].Descriptor, &left) != nil || json.Unmarshal(actual.Descriptor, &right) != nil {
		t.Fatal("descriptors must be JSON")
	}
	leftRaw, _ := json.Marshal(left)
	rightRaw, _ := json.Marshal(right)
	if !bytes.Equal(leftRaw, rightRaw) {
		t.Fatalf("runtime descriptor differs from signed manifest:\nwant=%s\ngot=%s", leftRaw, rightRaw)
	}
}

func TestMigrationIsResumableVerifiedAndReleaseQuarantines(t *testing.T) {
	root := filepath.Join(t.TempDir(), "provider")
	service, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	source, err := service.Provision("repository.primary")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(source.MountPath, "objects"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source.MountPath, "objects", "one"), []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	request := artifactstorage.VolumeMigrationRequest{MigrationID: "repository.move-001", SourceVolumeID: "repository.primary", TargetVolumeID: "repository.next", Phase: artifactstorage.MigrationPrepare}
	prepared, err := service.Migrate(context.Background(), request)
	if err != nil || !prepared.Ready || prepared.Target.MountPath == "" {
		t.Fatalf("prepare failed: result=%+v err=%v", prepared, err)
	}
	request.Phase = artifactstorage.MigrationSync
	first, err := service.Migrate(context.Background(), request)
	if err != nil || first.Files != 1 || first.Bytes != 5 || first.Digest == "" {
		t.Fatalf("sync failed: result=%+v err=%v", first, err)
	}
	if err := os.WriteFile(filepath.Join(source.MountPath, "objects", "two"), []byte("second"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := service.Migrate(context.Background(), request)
	if err != nil || second.Files != 2 || second.Digest == first.Digest {
		t.Fatalf("resumed sync did not converge: first=%+v second=%+v err=%v", first, second, err)
	}
	request.Phase = artifactstorage.MigrationVerify
	if _, err := service.Migrate(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(prepared.Target.MountPath, "unexpected"), []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Migrate(context.Background(), request); err == nil {
		t.Fatal("verify must reject target drift")
	}
	if _, err := service.Release(artifactstorage.VolumeReleaseRequest{MigrationID: request.MigrationID, VolumeID: source.VolumeID, ExpectedHandle: source.Handle}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(source.MountPath); !os.IsNotExist(err) {
		t.Fatalf("released source must leave active namespace: %v", err)
	}
}

func TestMigrationRejectsNonEmptyUnboundTargetAndSymlink(t *testing.T) {
	root := filepath.Join(t.TempDir(), "provider")
	service, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	source, _ := service.Provision("repository.primary")
	target, _ := service.Provision("repository.next")
	if err := os.WriteFile(filepath.Join(target.MountPath, "foreign"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	request := artifactstorage.VolumeMigrationRequest{MigrationID: "repository.move-002", SourceVolumeID: source.VolumeID, TargetVolumeID: target.VolumeID, Phase: artifactstorage.MigrationPrepare}
	if _, err := service.Migrate(context.Background(), request); err == nil {
		t.Fatal("unbound non-empty target must be rejected")
	}
	if err := os.Remove(filepath.Join(target.MountPath, "foreign")); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Migrate(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("outside", filepath.Join(source.MountPath, "link")); err != nil {
		t.Fatal(err)
	}
	request.Phase = artifactstorage.MigrationSync
	if _, err := service.Migrate(context.Background(), request); err == nil {
		t.Fatal("source symlink must be rejected")
	}
}

func TestNewRejectsWorldReadableRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "provider")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := New(root); err == nil {
		t.Fatal("非私有 provider root 必须拒绝")
	}
}
