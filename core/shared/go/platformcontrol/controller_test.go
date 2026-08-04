package platformcontrol

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	platformcontrolv1 "cdsoft.com.cn/VastPlan/contracts/schemas/platformcontrol/v1"
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

func TestControllerConfiguresThenBindsSQLSharedStateAtomically(t *testing.T) {
	root := t.TempDir()
	profileStore := &FileProfileStore{Path: filepath.Join(root, "platform-control.json")}
	stateStore, _ := sharedstate.OpenFileStore(filepath.Join(root, "provider-state.json"))
	bootstrapper := &fakeBootstrapper{store: managedTestStore{Store: stateStore}}
	binding := sharedstate.NewBindingStore()
	controller, err := NewController(profileStore, func(platformcontrolv1.SecretRef) (SecretSource, error) {
		return staticSecretSource{value: []byte("secret")}, nil
	}, bootstrapper, binding)
	if err != nil {
		t.Fatal(err)
	}
	if err := controller.Start(context.Background()); err != nil || controller.Status().Phase != platformcontrolv1.PhaseUnconfigured {
		t.Fatalf("空环境应进入未配置态: %+v %v", controller.Status(), err)
	}
	profile := testProfile(filepath.Join(root, "password"), 1)
	if err := controller.Configure(context.Background(), profile, 0); err != nil {
		t.Fatal(err)
	}
	if status := controller.Status(); status.Phase != platformcontrolv1.PhaseReady || status.Generation != 1 {
		t.Fatalf("配置完成状态错误: %+v", status)
	}
	if generation, _, ready := binding.Snapshot(); generation != 1 || !ready || bootstrapper.tested != 1 || bootstrapper.initialized != 1 {
		t.Fatalf("初始化调用链错误: generation=%d ready=%v bootstrapper=%+v", generation, ready, bootstrapper)
	}
	bootstrapper.testErr = errors.New("candidate database unavailable")
	if err := controller.Configure(context.Background(), testProfile(filepath.Join(root, "password-next"), 2), 1); err == nil {
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
	}, bootstrapper, binding)
	if err := controller.Configure(context.Background(), testProfile(filepath.Join(root, "password"), 1), 0); err == nil {
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
}

func TestControllerTestsCandidateWithoutInitializingOrCommitting(t *testing.T) {
	root := t.TempDir()
	profiles := &FileProfileStore{Path: filepath.Join(root, "platform-control.json")}
	binding := sharedstate.NewBindingStore()
	bootstrapper := &fakeBootstrapper{}
	controller, err := NewController(profiles, func(platformcontrolv1.SecretRef) (SecretSource, error) {
		return staticSecretSource{value: []byte("secret")}, nil
	}, bootstrapper, binding)
	if err != nil {
		t.Fatal(err)
	}
	if err := controller.TestCandidate(context.Background(), testProfile(filepath.Join(root, "password"), 1), 0); err != nil {
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
