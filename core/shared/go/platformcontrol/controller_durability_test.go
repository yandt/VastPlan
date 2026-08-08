package platformcontrol

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	platformcontrolv1 "cdsoft.com.cn/VastPlan/contracts/schemas/platformcontrol/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/sharedstate"
)

// commitProfileForTest lands a real profile on disk so the tests below can
// corrupt it the way an operator, a backup restore or a version rollback would.
func commitProfileForTest(t *testing.T, root string) (*FileProfileStore, string) {
	t.Helper()
	path := filepath.Join(root, "platform-control.json")
	store := &FileProfileStore{Path: path}
	secret := filepath.Join(root, "password")
	if err := os.WriteFile(secret+".secret", []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.Commit(context.Background(), testProfile(secret, 1), 0); err != nil {
		t.Fatal(err)
	}
	return store, path
}

func startedControllerForTest(t *testing.T, profiles ProfileStore, root string) (*Controller, *sharedstate.BindingStore, error) {
	t.Helper()
	binding := sharedstate.NewBindingStore()
	stateStore, _ := sharedstate.OpenFileStore(filepath.Join(root, "provider-state.json"))
	controller, err := NewController(
		profiles,
		func(platformcontrolv1.SecretRef) (SecretSource, error) {
			return staticSecretSource{value: []byte("secret")}, nil
		},
		&fakeBootstrapper{store: managedTestStore{Store: stateStore}},
		binding,
		&FileSecretMaterialStore{Root: filepath.Join(root, "managed-secrets")},
	)
	if err != nil {
		t.Fatal(err)
	}
	return controller, binding, controller.Start(context.Background())
}

// A profile whose permissions were widened is unreadable, but the platform was
// still configured. Reporting the unconfigured state here would reopen the seed
// authentication fallback on a platform that already holds real data.
func TestStartFailsClosedWhenCommittedProfileHasWidenedPermissions(t *testing.T) {
	root := t.TempDir()
	profiles, path := commitProfileForTest(t, root)
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}

	controller, binding, err := startedControllerForTest(t, profiles, root)
	if err == nil {
		t.Fatal("owner-only 校验失败的 Profile 必须让 Start 报错")
	}
	if _, probeErr := binding.Get(context.Background(), sharedstate.Scope{}, "probe"); !errors.Is(probeErr, sharedstate.ErrUnavailable) {
		t.Fatalf("已提交 Profile 不可读时必须 fail-closed 为 unavailable，不得回退未配置: %v", probeErr)
	}
	if status := controller.Status(); status.Phase != platformcontrolv1.PhaseRecovery {
		t.Fatalf("状态应为 recovery: %+v", status)
	}
}

// A profile written by a newer kernel and then rolled back fails schema
// validation. Same invariant: existence is durable, parsability is not.
func TestStartFailsClosedWhenCommittedProfileIsUnparsable(t *testing.T) {
	root := t.TempDir()
	profiles, path := commitProfileForTest(t, root)
	if err := os.WriteFile(path, []byte(`{"schemaVersion":1,"generation":1,"unknown":`), 0o600); err != nil {
		t.Fatal(err)
	}

	controller, binding, err := startedControllerForTest(t, profiles, root)
	if err == nil {
		t.Fatal("无法解析的 Profile 必须让 Start 报错")
	}
	if _, probeErr := binding.Get(context.Background(), sharedstate.Scope{}, "probe"); !errors.Is(probeErr, sharedstate.ErrUnavailable) {
		t.Fatalf("已提交但无法解析的 Profile 必须 fail-closed 为 unavailable，不得回退未配置: %v", probeErr)
	}
	if status := controller.Status(); status.Phase != platformcontrolv1.PhaseRecovery {
		t.Fatalf("状态应为 recovery: %+v", status)
	}
}

// unsyncedProfileStore commits for real, then reports the post-rename fsync
// failure. The profile is durably visible, so the controller must not roll the
// commit back.
type unsyncedProfileStore struct{ *FileProfileStore }

func (s unsyncedProfileStore) Commit(ctx context.Context, candidate platformcontrolv1.Profile, expected uint64) error {
	if err := s.FileProfileStore.Commit(ctx, candidate, expected); err != nil {
		return err
	}
	return ErrCommittedButUnsynced
}

// A directory fsync failure after the atomic rename used to delete the secret
// the persisted profile references and restore the unconfigured state, leaving
// a generation that no later Configure could repair.
func TestConfigureTreatsUnsyncedProfileAsCommitted(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "platform-control.json")
	profiles := unsyncedProfileStore{FileProfileStore: &FileProfileStore{Path: path}}
	binding := sharedstate.NewBindingStore()
	stateStore, _ := sharedstate.OpenFileStore(filepath.Join(root, "provider-state.json"))
	controller, err := NewController(
		profiles,
		func(platformcontrolv1.SecretRef) (SecretSource, error) {
			return staticSecretSource{value: []byte("secret")}, nil
		},
		&fakeBootstrapper{store: managedTestStore{Store: stateStore}},
		binding,
		&FileSecretMaterialStore{Root: filepath.Join(root, "managed-secrets")},
	)
	if err != nil {
		t.Fatal(err)
	}

	// One-time material makes the host own the secret file, which is the path
	// where the rollback used to delete a file the committed profile referenced.
	candidate := testProfile(filepath.Join(root, "password"), 1)
	candidate.SecretRef = platformcontrolv1.SecretRef{}
	request := platformcontrolv1.ChangeRequest{Profile: candidate, SecretMaterial: "password"}
	if err := controller.Configure(context.Background(), request); err != nil {
		t.Fatalf("rename 之后的 fsync 失败不得被当作提交失败: %v", err)
	}

	status := controller.Status()
	if status.Phase != platformcontrolv1.PhaseReady || status.Generation != 1 {
		t.Fatalf("Profile 已落盘时必须进入 Ready: %+v", status)
	}
	if status.Code != CodeProfileUnsynced {
		t.Fatalf("未 fsync 的事实必须仍可观测: %+v", status)
	}
	committed, err := profiles.Load(context.Background())
	if err != nil || committed == nil {
		t.Fatalf("Profile 必须留在磁盘上: %v", err)
	}
	if _, err := os.Lstat(committed.SecretRef.Path); err != nil {
		t.Fatalf("已提交 Profile 引用的密码文件不得被回滚删除: %v", err)
	}
	if _, _, ready := binding.Snapshot(); !ready {
		t.Fatal("提交成功后必须完成绑定")
	}
}
