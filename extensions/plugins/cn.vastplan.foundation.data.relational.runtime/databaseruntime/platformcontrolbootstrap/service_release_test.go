package platformcontrolbootstrap

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	platformcontrolv1 "cdsoft.com.cn/VastPlan/contracts/schemas/platformcontrol/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/sharedstate"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.foundation.data.relational.runtime/databaseruntime"
)

func releaseTestService(t *testing.T) (*Service, string) {
	t.Helper()
	provider := &bootstrapProvider{}
	registry := databaseruntime.NewRegistry()
	if err := registry.Register(provider); err != nil {
		t.Fatal(err)
	}
	bootstrapper, _ := New(registry)
	service, err := NewService(bootstrapper, sharedstate.NewBindingStore(), databaseruntime.NewPlatformRecordBinding(),
		func(context.Context, databaseruntime.PlatformRecordStore) error { return nil }, "")
	if err != nil {
		t.Fatal(err)
	}
	secretPath := filepath.Join(t.TempDir(), "password")
	if err := os.WriteFile(secretPath, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	return service, secretPath
}

func trustedCallContext() *contractv1.CallContext {
	return &contractv1.CallContext{Caller: &contractv1.Caller{
		Kind: contractv1.CallerKind_CALLER_KIND_SYSTEM,
		Id:   platformcontrolv1.TrustedBootstrapSystemID,
	}}
}

func openGeneration(t *testing.T, service *Service, secretPath string, generation uint64) {
	t.Helper()
	profile := bootstrapProfile("mysql", "db.internal:3306", "platform", "platform", "vastplan", "verify-ca", secretPath)
	profile.Generation = generation
	payload, _ := json.Marshal(profile)
	handler := service.Contribution().Handlers[platformcontrolv1.OperationInitialize]
	result, _, err := handler(context.Background(), nil, trustedCallContext(), payload)
	if err != nil || result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("generation %d 初始化失败: result=%+v err=%v", generation, result, err)
	}
}

func callClose(t *testing.T, service *Service, call *contractv1.CallContext, generation uint64) *contractv1.CallResult {
	t.Helper()
	payload, _ := json.Marshal(platformcontrolv1.CloseRequest{Generation: generation})
	handler := service.Contribution().Handlers[platformcontrolv1.OperationClose]
	if handler == nil {
		t.Fatal("Bootstrap Capability 必须暴露 close 操作")
	}
	result, _, err := handler(context.Background(), nil, call, payload)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

// Releasing the generation the replica actually holds must drop the pool.
func TestCloseReleasesMatchingGeneration(t *testing.T) {
	service, secretPath := releaseTestService(t)
	defer service.Close()
	openGeneration(t, service, secretPath, 1)

	if service.managed == nil || service.managedGeneration != 1 {
		t.Fatalf("初始化后必须持有 generation 1 的池: managed=%v generation=%d", service.managed != nil, service.managedGeneration)
	}
	if result := callClose(t, service, trustedCallContext(), 1); result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("释放匹配 generation 必须成功: %+v", result)
	}
	if service.managed != nil || service.managedGeneration != 0 {
		t.Fatal("释放后必须清空持有的候选池")
	}

	// Idempotent: a repeated release is a no-op, not an error.
	if result := callClose(t, service, trustedCallContext(), 1); result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("重复释放必须幂等: %+v", result)
	}
}

// A stale release must never close the pool a later activation installed —
// that pool is actively serving Shared State.
func TestCloseIgnoresStaleGeneration(t *testing.T) {
	service, secretPath := releaseTestService(t)
	defer service.Close()
	openGeneration(t, service, secretPath, 1)
	openGeneration(t, service, secretPath, 2)

	if service.managedGeneration != 2 {
		t.Fatalf("应持有 generation 2: %d", service.managedGeneration)
	}
	if result := callClose(t, service, trustedCallContext(), 1); result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("陈旧释放应静默无操作: %+v", result)
	}
	if service.managed == nil || service.managedGeneration != 2 {
		t.Fatal("陈旧释放不得关闭当前正在服务的池")
	}
}

// The release path carries no profile and no secret, so the caller check is the
// only thing standing between a plugin and a live connection pool.
func TestCloseRejectsUntrustedCallerAndInvalidPayload(t *testing.T) {
	service, secretPath := releaseTestService(t)
	defer service.Close()
	openGeneration(t, service, secretPath, 1)

	untrusted := &contractv1.CallContext{Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: "example"}}
	if result := callClose(t, service, untrusted, 1); result.GetError().GetCode() != platformcontrolv1.ErrorInvalid {
		t.Fatalf("普通插件必须被拒绝: %+v", result)
	}
	if service.managed == nil {
		t.Fatal("被拒绝的释放不得关闭池")
	}
	if result := callClose(t, service, trustedCallContext(), 0); result.GetError().GetCode() != platformcontrolv1.ErrorInvalid {
		t.Fatalf("generation 0 必须被拒绝: %+v", result)
	}
	if service.managed == nil {
		t.Fatal("无效 payload 不得关闭池")
	}
}
