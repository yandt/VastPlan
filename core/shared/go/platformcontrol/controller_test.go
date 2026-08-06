package platformcontrol

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
	platformcontrolv1 "cdsoft.com.cn/VastPlan/contracts/schemas/platformcontrol/v1"
	platformcontrolport "cdsoft.com.cn/VastPlan/extensions/libraries/go/platformcontrol"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/sharedstate"
)

type staticSecretSource struct{ value []byte }

func (s staticSecretSource) WithSecret(_ context.Context, use func([]byte) error) error {
	value := append([]byte(nil), s.value...)
	defer clear(value)
	return use(value)
}

type fakeBootstrapper struct {
	store         ManagedStore
	testErr       error
	initializeErr error
	openErr       error
	tested        int
	initialized   int
	opened        int
}

// profileSecretBootstrapper simulates the real cross-process Database Runtime:
// it cannot use the host's in-memory SecretSource and must reopen the secret
// from the reference carried by the wire Profile.
type profileSecretBootstrapper struct{}

func (profileSecretBootstrapper) Test(ctx context.Context, profile platformcontrolv1.Profile, _ SecretSource) error {
	source, err := ResolveSecretSource(profile.SecretRef, "")
	if err != nil {
		return err
	}
	return source.WithSecret(ctx, func(value []byte) error {
		if string(value) != "direct-password" {
			return errors.New("unexpected direct password")
		}
		return nil
	})
}

func (profileSecretBootstrapper) Initialize(context.Context, platformcontrolv1.Profile, SecretSource) (ManagedStore, error) {
	return nil, errors.New("not used")
}

func (profileSecretBootstrapper) Open(context.Context, platformcontrolv1.Profile, SecretSource) (ManagedStore, error) {
	return nil, errors.New("not used")
}

func (f *fakeBootstrapper) Test(ctx context.Context, _ platformcontrolv1.Profile, secret SecretSource) error {
	f.tested++
	if f.testErr != nil {
		return f.testErr
	}
	return secret.WithSecret(ctx, func(value []byte) error {
		if len(value) == 0 {
			return errors.New("empty secret")
		}
		return nil
	})
}

func (f *fakeBootstrapper) Initialize(context.Context, platformcontrolv1.Profile, SecretSource) (ManagedStore, error) {
	f.initialized++
	return f.store, f.initializeErr
}

func (f *fakeBootstrapper) Open(context.Context, platformcontrolv1.Profile, SecretSource) (ManagedStore, error) {
	f.opened++
	return f.store, f.openErr
}

type managedTestStore struct{ sharedstate.Store }

func (managedTestStore) Close() error { return nil }

type rejectingProfileStore struct{}

func (rejectingProfileStore) Load(context.Context) (*platformcontrolv1.Profile, error) {
	return nil, nil
}

func (rejectingProfileStore) Commit(context.Context, platformcontrolv1.Profile, uint64) error {
	return ErrGenerationConflict
}

func TestControllerConfiguresThenBindsSQLSharedStateAtomically(t *testing.T) {
	root := t.TempDir()
	profileStore := &FileProfileStore{Path: filepath.Join(root, "platform-control.json")}
	stateStore, _ := sharedstate.OpenFileStore(filepath.Join(root, "provider-state.json"))
	bootstrapper := &fakeBootstrapper{store: managedTestStore{Store: stateStore}}
	binding := sharedstate.NewBindingStore()
	controller, err := NewController(profileStore, func(platformcontrolv1.SecretRef) (SecretSource, error) {
		return staticSecretSource{value: []byte("secret")}, nil
	}, bootstrapper, binding, &FileSecretMaterialStore{Root: filepath.Join(root, "managed-secrets")})
	if err != nil {
		t.Fatal(err)
	}
	if err := controller.Start(context.Background()); err != nil || controller.Status().Phase != platformcontrolv1.PhaseUnconfigured {
		t.Fatalf("空环境应进入未配置态: %+v %v", controller.Status(), err)
	}
	if _, err := binding.Get(context.Background(), sharedstate.Scope{}, "probe"); !errors.Is(err, sharedstate.ErrUnconfigured) {
		t.Fatalf("未提交 Profile 时 Shared State 必须保持未配置语义: %v", err)
	}
	profile := testProfile(filepath.Join(root, "password"), 1)
	if err := controller.Configure(context.Background(), platformcontrolv1.ChangeRequest{Profile: profile}); err != nil {
		t.Fatal(err)
	}
	if status := controller.Status(); status.Phase != platformcontrolv1.PhaseReady || status.Generation != 1 {
		t.Fatalf("配置完成状态错误: %+v", status)
	}
	if generation, _, ready := binding.Snapshot(); generation != 1 || !ready || bootstrapper.tested != 1 || bootstrapper.initialized != 1 {
		t.Fatalf("初始化调用链错误: generation=%d ready=%v bootstrapper=%+v", generation, ready, bootstrapper)
	}
	bootstrapper.testErr = errors.New("candidate database unavailable")
	if err := controller.Configure(context.Background(), platformcontrolv1.ChangeRequest{Profile: testProfile(filepath.Join(root, "password-next"), 2), ExpectedGeneration: 1}); err == nil {
		t.Fatal("失败候选必须返回错误")
	}
	if status := controller.Status(); status.Phase != platformcontrolv1.PhaseReady || status.Generation != 1 || status.Code != CodeDatabaseUnavailable {
		t.Fatalf("失败候选必须保留旧代 Ready: %+v", status)
	}
	if generation, _, ready := binding.Snapshot(); generation != 1 || !ready {
		t.Fatal("失败候选不得替换旧 Store")
	}
}

func TestControllerFailsClosedWithoutCommittingOrFallback(t *testing.T) {
	root := t.TempDir()
	profiles := &FileProfileStore{Path: filepath.Join(root, "platform-control.json")}
	binding := sharedstate.NewBindingStore()
	bootstrapper := &fakeBootstrapper{testErr: errors.New("database unavailable")}
	controller, _ := NewController(profiles, func(platformcontrolv1.SecretRef) (SecretSource, error) {
		return staticSecretSource{value: []byte("secret")}, nil
	}, bootstrapper, binding, &FileSecretMaterialStore{Root: filepath.Join(root, "managed-secrets")})
	if err := controller.Configure(context.Background(), platformcontrolv1.ChangeRequest{Profile: testProfile(filepath.Join(root, "password"), 1)}); err == nil {
		t.Fatal("连接测试失败必须阻断配置")
	}
	if status := controller.Status(); status.Phase != platformcontrolv1.PhaseRecovery || status.Code != CodeDatabaseUnavailable {
		t.Fatalf("失败状态错误: %+v", status)
	}
	if profile, _ := profiles.Load(context.Background()); profile != nil {
		t.Fatal("失败候选不得提交 Profile")
	}
	if _, _, ready := binding.Snapshot(); ready {
		t.Fatal("失败时不得绑定本地 JSON 或候选 Store")
	}
	if _, err := binding.Get(context.Background(), sharedstate.Scope{}, "probe"); !errors.Is(err, sharedstate.ErrUnconfigured) {
		t.Fatalf("未提交的失败候选不得关闭 Seed bootstrap 路径: %v", err)
	}
}

func TestControllerPreservesRemoteDatabaseFailureCode(t *testing.T) {
	root := t.TempDir()
	controller, _ := NewController(
		&FileProfileStore{Path: filepath.Join(root, "platform-control.json")},
		func(platformcontrolv1.SecretRef) (SecretSource, error) {
			return staticSecretSource{value: []byte("secret")}, nil
		},
		&fakeBootstrapper{testErr: platformcontrolport.NewFailure(databasev1.ErrorAuthenticationFailed, false)},
		sharedstate.NewBindingStore(),
		&FileSecretMaterialStore{Root: filepath.Join(root, "managed-secrets")},
	)
	err := controller.TestCandidate(context.Background(), platformcontrolv1.ChangeRequest{Profile: testProfile(filepath.Join(root, "password"), 1)})
	if err == nil || controller.Status().Code != databasev1.ErrorAuthenticationFailed {
		t.Fatalf("Controller 覆盖了 Runtime 数据库诊断: status=%+v err=%v", controller.Status(), err)
	}
}

func TestControllerExistingProfileFailureRequiresProviderAndFailsClosed(t *testing.T) {
	root := t.TempDir()
	profiles := &FileProfileStore{Path: filepath.Join(root, "platform-control.json")}
	profile := testProfile(filepath.Join(root, "password"), 1)
	if err := profiles.Commit(context.Background(), profile, 0); err != nil {
		t.Fatal(err)
	}
	binding := sharedstate.NewBindingStore()
	bootstrapper := &fakeBootstrapper{openErr: errors.New("database unavailable")}
	controller, _ := NewController(profiles, func(platformcontrolv1.SecretRef) (SecretSource, error) {
		return staticSecretSource{value: []byte("secret")}, nil
	}, bootstrapper, binding, &FileSecretMaterialStore{Root: filepath.Join(root, "managed-secrets")})
	if err := controller.Start(context.Background()); err == nil {
		t.Fatal("已配置数据库打开失败必须返回错误")
	}
	if _, err := binding.Get(context.Background(), sharedstate.Scope{}, "probe"); !errors.Is(err, sharedstate.ErrUnavailable) {
		t.Fatalf("已提交 Profile 后不得重新进入未配置回退: %v", err)
	}
}

func TestControllerTestsCandidateWithoutInitializingOrCommitting(t *testing.T) {
	root := t.TempDir()
	profiles := &FileProfileStore{Path: filepath.Join(root, "platform-control.json")}
	binding := sharedstate.NewBindingStore()
	bootstrapper := &fakeBootstrapper{}
	controller, err := NewController(profiles, func(platformcontrolv1.SecretRef) (SecretSource, error) {
		return staticSecretSource{value: []byte("secret")}, nil
	}, bootstrapper, binding, &FileSecretMaterialStore{Root: filepath.Join(root, "managed-secrets")})
	if err != nil {
		t.Fatal(err)
	}
	if err := controller.TestCandidate(context.Background(), platformcontrolv1.ChangeRequest{Profile: testProfile(filepath.Join(root, "password"), 1)}); err != nil {
		t.Fatal(err)
	}
	if bootstrapper.tested != 1 || bootstrapper.initialized != 0 || bootstrapper.opened != 0 {
		t.Fatalf("测试候选不得初始化或打开 Store: %+v", bootstrapper)
	}
	if profile, err := profiles.Load(context.Background()); err != nil || profile != nil {
		t.Fatalf("测试候选不得提交 Profile: profile=%+v err=%v", profile, err)
	}
	if _, _, ready := binding.Snapshot(); ready {
		t.Fatal("测试候选不得绑定 Shared State")
	}
	if status := controller.Status(); status.Phase != platformcontrolv1.PhaseUnconfigured || status.Generation != 0 {
		t.Fatalf("空环境测试成功后应恢复未配置状态: %+v", status)
	}
}

func TestControllerStagesDirectPasswordAsManagedReference(t *testing.T) {
	root := t.TempDir()
	profiles := &FileProfileStore{Path: filepath.Join(root, "platform-control.json")}
	stateStore, _ := sharedstate.OpenFileStore(filepath.Join(root, "provider-state.json"))
	bootstrapper := &fakeBootstrapper{store: managedTestStore{Store: stateStore}}
	binding := sharedstate.NewBindingStore()
	materials := &FileSecretMaterialStore{Root: filepath.Join(root, "managed-secrets")}
	controller, err := NewController(profiles, func(platformcontrolv1.SecretRef) (SecretSource, error) {
		return nil, errors.New("direct password must not use external resolver")
	}, bootstrapper, binding, materials)
	if err != nil {
		t.Fatal(err)
	}
	profile := testProfile(filepath.Join(root, "unused-password-file"), 1)
	profile.SecretRef = platformcontrolv1.SecretRef{}
	request := platformcontrolv1.ChangeRequest{Profile: profile, SecretMaterial: "direct-password"}
	if err := controller.Configure(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	stored, err := profiles.Load(context.Background())
	if err != nil || stored == nil || stored.SecretRef.Kind != "owner-file" || filepath.Dir(stored.SecretRef.Path) != materials.Root {
		t.Fatalf("Profile 必须只保存托管引用: profile=%+v err=%v", stored, err)
	}
	raw, err := os.ReadFile(profiles.Path)
	if err != nil || strings.Contains(string(raw), "direct-password") {
		t.Fatalf("Profile 不得包含密码明文: err=%v", err)
	}
	source, err := ResolveSecretSource(stored.SecretRef, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := source.WithSecret(context.Background(), func(value []byte) error {
		if string(value) != "direct-password" {
			t.Fatal("托管密码内容错误")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestControllerRollsBackManagedPasswordWhenProfileCommitFails(t *testing.T) {
	root := t.TempDir()
	managedRoot := filepath.Join(root, "managed-secrets")
	stateStore, _ := sharedstate.OpenFileStore(filepath.Join(root, "provider-state.json"))
	controller, err := NewController(
		rejectingProfileStore{},
		func(platformcontrolv1.SecretRef) (SecretSource, error) {
			return nil, errors.New("direct password must not use external resolver")
		},
		&fakeBootstrapper{store: managedTestStore{Store: stateStore}},
		sharedstate.NewBindingStore(),
		&FileSecretMaterialStore{Root: managedRoot},
	)
	if err != nil {
		t.Fatal(err)
	}
	profile := testProfile(filepath.Join(root, "unused-password-file"), 1)
	profile.SecretRef = platformcontrolv1.SecretRef{}
	if err := controller.Configure(context.Background(), platformcontrolv1.ChangeRequest{
		Profile: profile, SecretMaterial: "direct-password",
	}); !errors.Is(err, ErrGenerationConflict) {
		t.Fatalf("Profile CAS 失败应原样返回: %v", err)
	}
	entries, err := os.ReadDir(managedRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("Profile 提交失败后不得保留托管密码: %+v", entries)
	}
}

func TestControllerDirectPasswordTestDoesNotPersistMaterial(t *testing.T) {
	root := t.TempDir()
	managedRoot := filepath.Join(root, "managed-secrets")
	profile := testProfile(filepath.Join(root, "unused-password-file"), 1)
	profile.SecretRef = platformcontrolv1.SecretRef{}
	controller, err := NewController(
		&FileProfileStore{Path: filepath.Join(root, "platform-control.json")},
		func(platformcontrolv1.SecretRef) (SecretSource, error) {
			return nil, errors.New("direct password must not use external resolver")
		},
		profileSecretBootstrapper{},
		sharedstate.NewBindingStore(),
		&FileSecretMaterialStore{Root: managedRoot},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := controller.TestCandidate(context.Background(), platformcontrolv1.ChangeRequest{
		Profile: profile, SecretMaterial: "direct-password",
	}); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(managedRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("仅测试连接不得持久化密码: %+v", entries)
	}
}
