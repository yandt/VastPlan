package settings

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func TestDescriptorMatchesSignedManifest(t *testing.T) {
	assertDescriptorMatchesManifest(t, Capability, Descriptor())
}

func assertDescriptorMatchesManifest(t *testing.T, capability string, descriptor []byte) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "vastplan.plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := pluginv1.ParseManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	contributions, err := pluginv1.BackendRuntimeContributions(manifest)
	if err != nil {
		t.Fatal(err)
	}
	for _, contribution := range contributions {
		if contribution.ID != capability {
			continue
		}
		var signed, runtime any
		if json.Unmarshal(contribution.Descriptor, &signed) != nil || json.Unmarshal(descriptor, &runtime) != nil || !reflect.DeepEqual(signed, runtime) {
			t.Fatalf("运行时 descriptor 与签名 Manifest 不一致\nsigned=%s\nruntime=%s", contribution.Descriptor, descriptor)
		}
		return
	}
	t.Fatalf("Manifest 未声明 capability %s", capability)
}

func testCallContext(tenant string) *contractv1.CallContext {
	return &contractv1.CallContext{TenantId: tenant}
}

func TestSettingsPersistsTenantScopedCASAndChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	service, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := testCallContext("tenant-a")
	created, err := service.Put(ctx, "feature.enabled", json.RawMessage(`true`), ptr(int64(0)))
	if err != nil || created.Version != 1 {
		t.Fatalf("首次写入失败: value=%+v err=%v", created, err)
	}
	if _, err := service.Put(ctx, "feature.enabled", json.RawMessage(`false`), ptr(int64(0))); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("错误 CAS 前置条件必须拒绝: %v", err)
	}
	updated, err := service.Put(ctx, "feature.enabled", json.RawMessage(`false`), ptr(created.Version))
	if err != nil || updated.Version != 2 {
		t.Fatalf("更新失败: value=%+v err=%v", updated, err)
	}
	if _, err := service.Put(testCallContext("tenant-b"), "feature.enabled", json.RawMessage(`"other"`), nil); err != nil {
		t.Fatal(err)
	}
	reopened, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := reopened.Get(ctx, "feature.enabled")
	if err != nil || string(got.Value) != "false" || got.Version != 2 {
		t.Fatalf("持久化读取错误: value=%+v err=%v", got, err)
	}
	changes, err := reopened.ChangesSince(ctx, 0)
	if err != nil || len(changes) != 2 || changes[1].Version != 2 {
		t.Fatalf("变更流错误: changes=%+v err=%v", changes, err)
	}
	if _, err := reopened.Get(testCallContext("tenant-b"), "feature.enabled"); err != nil {
		t.Fatal(err)
	}
}

type configHost struct{ stateFile string }

var _ sdk.Host = configHost{}

func (h configHost) Call(_ context.Context, target *contractv1.CallTarget, _ *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
	if target.GetCapability() != "kernel.config.get" {
		return nil, nil, errors.New("unexpected capability")
	}
	var request map[string]string
	if err := json.Unmarshal(payload, &request); err != nil || request["key"] != StateFileConfigKey {
		return nil, nil, errors.New("unexpected config request")
	}
	raw, _ := json.Marshal(h.stateFile)
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
}

func TestHandlerGetsStateFileFromAuthenticatedHostConfig(t *testing.T) {
	service, err := New("")
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte(`{"key":"system.theme","value":"dark"}`)
	result, raw, err := service.Handler(context.Background(), configHost{stateFile: filepath.Join(t.TempDir(), "settings.json")}, testCallContext("tenant-a"), payload, "put")
	if err != nil || result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("通过宿主配置初始化失败: result=%+v err=%v", result, err)
	}
	if !json.Valid(raw) {
		t.Fatalf("响应不是 JSON: %s", raw)
	}
}

func ptr(value int64) *int64 { return &value }
