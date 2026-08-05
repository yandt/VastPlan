package platformcontrolbootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	platformcontrolv1 "cdsoft.com.cn/VastPlan/contracts/schemas/platformcontrol/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/sharedstate"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.foundation.data.relational.runtime/databaseruntime"
)

func TestServiceAllowsOnlyTrustedHostAndSwitchesGeneration(t *testing.T) {
	provider := &bootstrapProvider{}
	registry := databaseruntime.NewRegistry()
	if err := registry.Register(provider); err != nil {
		t.Fatal(err)
	}
	bootstrapper, _ := New(registry)
	binding := sharedstate.NewBindingStore()
	recordBinding := databaseruntime.NewPlatformRecordBinding()
	prepared := 0
	service, _ := NewService(bootstrapper, binding, recordBinding, func(context.Context, databaseruntime.PlatformRecordStore) error {
		prepared++
		return nil
	}, "")
	defer service.Close()

	secretPath := filepath.Join(t.TempDir(), "password")
	if err := os.WriteFile(secretPath, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	profile := bootstrapProfile("mysql", "db.internal:3306", "platform", "platform", "vastplan", "verify-ca", secretPath)
	payload, _ := json.Marshal(profile)
	handler := service.Contribution().Handlers[platformcontrolv1.OperationInitialize]
	untrusted := &contractv1.CallContext{Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: "example"}}
	result, _, err := handler(context.Background(), nil, untrusted, payload)
	if err != nil || result.GetError().GetCode() != platformcontrolv1.ErrorInvalid {
		t.Fatalf("普通插件必须被拒绝: result=%+v err=%v", result, err)
	}

	trusted := &contractv1.CallContext{Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_SYSTEM, Id: platformcontrolv1.TrustedBootstrapSystemID}}
	result, raw, err := handler(context.Background(), nil, trusted, payload)
	if err != nil || result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("可信初始化失败: result=%+v err=%v", result, err)
	}
	var status platformcontrolv1.Status
	if json.Unmarshal(raw, &status) != nil || status.Phase != platformcontrolv1.PhaseReady || status.Generation != 1 {
		t.Fatalf("初始化状态错误: %+v", status)
	}
	if generation, _, ready := binding.Snapshot(); generation != 1 || !ready {
		t.Fatalf("SQL Shared State 未绑定: generation=%d ready=%v", generation, ready)
	}
	if generation, _, ready := recordBinding.Snapshot(); generation != 1 || !ready {
		t.Fatalf("Platform Record Store 未绑定: generation=%d ready=%v", generation, ready)
	}
	if prepared != 1 {
		t.Fatalf("Platform DataModel 必须在绑定前准备一次: %d", prepared)
	}
	poolCount := len(provider.pools)
	if _, _, err := handler(context.Background(), nil, trusted, payload); err != nil || len(provider.pools) != poolCount {
		t.Fatal("同 generation 初始化必须幂等且不得重复开池")
	}

	profile.Generation = 2
	payload, _ = json.Marshal(profile)
	if result, _, err = handler(context.Background(), nil, trusted, payload); err != nil || result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("第二代初始化失败: result=%+v err=%v", result, err)
	}
	if !provider.pools[0].closed {
		t.Fatal("切换新 generation 后必须关闭旧池")
	}
}

func TestServiceTestDoesNotBindStore(t *testing.T) {
	provider := &bootstrapProvider{}
	registry := databaseruntime.NewRegistry()
	_ = registry.Register(provider)
	bootstrapper, _ := New(registry)
	binding := sharedstate.NewBindingStore()
	recordBinding := databaseruntime.NewPlatformRecordBinding()
	service, _ := NewService(bootstrapper, binding, recordBinding, func(context.Context, databaseruntime.PlatformRecordStore) error { return nil }, "")
	secretPath := filepath.Join(t.TempDir(), "password")
	_ = os.WriteFile(secretPath, []byte("secret"), 0o600)
	profile := bootstrapProfile("mysql", "db.internal:3306", "platform", "platform", "vastplan", "verify-ca", secretPath)
	payload, _ := json.Marshal(profile)
	trusted := &contractv1.CallContext{Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_SYSTEM, Id: platformcontrolv1.TrustedBootstrapSystemID}}
	result, _, err := service.Contribution().Handlers[platformcontrolv1.OperationTest](context.Background(), nil, trusted, payload)
	if err != nil || result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatal("连接测试失败")
	}
	if _, _, ready := binding.Snapshot(); ready {
		t.Fatal("连接测试不得绑定 Shared State")
	}
	if _, _, ready := recordBinding.Snapshot(); ready {
		t.Fatal("连接测试不得绑定 Platform Record Store")
	}
	if len(provider.pools) != 1 || !provider.pools[0].closed {
		t.Fatal("连接测试必须关闭候选池")
	}
}

func TestServiceDoesNotBindCandidateWhenPlatformModelPreparationFails(t *testing.T) {
	provider := &bootstrapProvider{}
	registry := databaseruntime.NewRegistry()
	_ = registry.Register(provider)
	bootstrapper, _ := New(registry)
	binding := sharedstate.NewBindingStore()
	recordBinding := databaseruntime.NewPlatformRecordBinding()
	service, _ := NewService(bootstrapper, binding, recordBinding, func(context.Context, databaseruntime.PlatformRecordStore) error {
		return errors.New("schema preparation failed")
	}, "")
	secretPath := filepath.Join(t.TempDir(), "password")
	_ = os.WriteFile(secretPath, []byte("secret"), 0o600)
	profile := bootstrapProfile("mysql", "db.internal:3306", "platform", "platform", "vastplan", "verify-ca", secretPath)
	payload, _ := json.Marshal(profile)
	trusted := &contractv1.CallContext{Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_SYSTEM, Id: platformcontrolv1.TrustedBootstrapSystemID}}
	result, _, err := service.Contribution().Handlers[platformcontrolv1.OperationInitialize](context.Background(), nil, trusted, payload)
	if err != nil || result.GetError().GetCode() != platformcontrolv1.ErrorInitializationFailed {
		t.Fatalf("Schema 准备失败必须拒绝候选: result=%+v err=%v", result, err)
	}
	if _, _, ready := binding.Snapshot(); ready {
		t.Fatal("Schema 准备失败不得绑定 Shared State")
	}
	if _, _, ready := recordBinding.Snapshot(); ready {
		t.Fatal("Schema 准备失败不得绑定 Platform Record Store")
	}
	if len(provider.pools) != 1 || !provider.pools[0].closed {
		t.Fatal("Schema 准备失败必须关闭候选池")
	}
}
