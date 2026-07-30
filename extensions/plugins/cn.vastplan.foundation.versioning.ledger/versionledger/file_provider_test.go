package versionledger

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	versioningv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versioning/v1"
)

func TestFileProviderContract(t *testing.T) {
	provider, err := openFileProvider(privateTempDir(t), fixedClock())
	if err != nil {
		t.Fatal(err)
	}
	runProviderContract(t, provider)
}

func TestFileProviderRecoversCommittedVersionsAndIgnoresCrashTemps(t *testing.T) {
	root := privateTempDir(t)
	provider, err := openFileProvider(root, fixedClock())
	if err != nil {
		t.Fatal(err)
	}
	scope := Scope{TenantID: "tenant-a"}
	first, err := provider.PutVersion(context.Background(), scope, putRequest("portal-main:revision:0001", nil, `{"layout":"standard"}`))
	if err != nil {
		t.Fatal(err)
	}
	versionsDir := findDirectory(t, root, "versions")
	if err := os.WriteFile(filepath.Join(versionsDir, ".version-crash-leftover"), []byte(`{"partial":`), 0o600); err != nil {
		t.Fatal(err)
	}

	reopened, err := openFileProvider(root, fixedClock())
	if err != nil {
		t.Fatal(err)
	}
	got, err := reopened.GetVersion(context.Background(), scope, versioningv1.GetVersionRequest{Ref: first.Version.Ref})
	if err != nil || got.Version.Ref != first.Version.Ref {
		t.Fatalf("重启后恢复版本失败: %+v %v", got, err)
	}
	second, err := reopened.PutVersion(context.Background(), scope, putRequest("portal-main:revision:0002", &first.Version.Ref, `{"layout":"compact"}`))
	if err != nil || second.Version.Ref.Sequence != 2 {
		t.Fatalf("恢复后 sequence 未延续: %+v %v", second, err)
	}
}

func TestFileProviderFailsClosedOnCommittedCorruption(t *testing.T) {
	root := privateTempDir(t)
	provider, err := openFileProvider(root, fixedClock())
	if err != nil {
		t.Fatal(err)
	}
	scope := Scope{TenantID: "tenant-a"}
	first, err := provider.PutVersion(context.Background(), scope, putRequest("portal-main:revision:0001", nil, `{"layout":"standard"}`))
	if err != nil {
		t.Fatal(err)
	}
	versionsDir := findDirectory(t, root, "versions")
	filename := filepath.Join(versionsDir, first.Version.Ref.VersionID+".json")
	raw, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	tampered := strings.Replace(string(raw), `"standard"`, `"attacker"`, 1)
	if err := os.WriteFile(filename, []byte(tampered), 0o600); err != nil {
		t.Fatal(err)
	}
	reopened, err := openFileProvider(root, fixedClock())
	if err != nil {
		t.Fatal(err)
	}
	_, err = reopened.GetVersion(context.Background(), scope, versioningv1.GetVersionRequest{Ref: first.Version.Ref})
	if errorCode(err) != versioningv1.ErrorCorrupted {
		t.Fatalf("损坏版本必须 fail-closed: %v", err)
	}
}

func TestFileProviderFailsClosedWhenHeadTargetIsMissing(t *testing.T) {
	root := privateTempDir(t)
	provider, err := openFileProvider(root, fixedClock())
	if err != nil {
		t.Fatal(err)
	}
	scope := Scope{TenantID: "tenant-a"}
	first, err := provider.PutVersion(context.Background(), scope, putRequest("portal-main:revision:0001", nil, `{}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.MoveHead(context.Background(), scope, versioningv1.MoveHeadRequest{Stream: first.Version.Ref.Stream, Name: "published", Target: first.Version.Ref}); err != nil {
		t.Fatal(err)
	}
	headsDir := findDirectory(t, root, "heads")
	filename := filepath.Join(headsDir, "published.json")
	raw, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	tampered := strings.Replace(string(raw), first.Version.Ref.VersionID, strings.Repeat("d", 64), 1)
	if err := os.WriteFile(filename, []byte(tampered), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = provider.GetHead(context.Background(), scope, versioningv1.GetHeadRequest{Stream: first.Version.Ref.Stream, Name: "published"})
	if errorCode(err) != versioningv1.ErrorCorrupted {
		t.Fatalf("指向缺失版本的 Head 必须 fail-closed: %v", err)
	}
}

func TestFileProviderRejectsInsecureOrDangerousRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenFileProvider(root); err == nil {
		t.Fatal("Provider 必须拒绝权限过宽的已有 root")
	}
	if _, err := OpenFileProvider("relative/path"); err == nil {
		t.Fatal("相对 File Provider root 必须被拒绝")
	}
	if _, err := OpenFileProvider(string(filepath.Separator)); err == nil {
		t.Fatal("文件系统根目录不得作为 File Provider root")
	}
}

func findDirectory(t *testing.T, root, base string) string {
	t.Helper()
	found := ""
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && entry.Name() == base {
			found = path
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if found == "" {
		t.Fatalf("未找到目录 %s", base)
	}
	return found
}

func privateTempDir(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	return root
}
