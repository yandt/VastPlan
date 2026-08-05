package platformcontrol

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestFileSecretMaterialStoreCommitsOwnerOnlyAndReconcilesOrphans(t *testing.T) {
	root := filepath.Join(t.TempDir(), "managed")
	store := &FileSecretMaterialStore{Root: root}
	prepared, err := store.Prepare(context.Background(), 1, []byte("direct-password"))
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Ref().Kind != "owner-file" || filepath.Dir(prepared.Ref().Path) != root {
		t.Fatalf("必须返回核心受控的 owner-file 引用: %+v", prepared.Ref())
	}
	if err := prepared.Source().WithSecret(context.Background(), func(value []byte) error {
		if string(value) != "direct-password" {
			t.Fatal("候选密码错误")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := prepared.Commit(); err != nil {
		t.Fatal(err)
	}
	path := prepared.Ref().Path
	if info, err := os.Lstat(path); err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Fatalf("托管密码必须是 0600 普通文件: info=%+v err=%v", info, err)
	}
	active := prepared.Ref()
	if err := store.Reconcile(&active); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("活动引用不得被清理: %v", err)
	}
	if err := store.Reconcile(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("无活动 Profile 时必须清理孤儿密码: %v", err)
	}
}

func TestPreparedSecretRollbackRemovesCandidateAndCommittedFile(t *testing.T) {
	root := filepath.Join(t.TempDir(), "managed")
	store := &FileSecretMaterialStore{Root: root}
	prepared, err := store.Prepare(context.Background(), 2, []byte("direct-password"))
	if err != nil {
		t.Fatal(err)
	}
	if err := prepared.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := prepared.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(prepared.Ref().Path); !os.IsNotExist(err) {
		t.Fatalf("回滚后不得保留密码文件: %v", err)
	}
}
