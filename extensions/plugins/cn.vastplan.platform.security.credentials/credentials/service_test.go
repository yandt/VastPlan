package credentials

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
)

func TestDescriptorMatchesSignedManifest(t *testing.T) {
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
	var signed, runtime any
	if len(contributions) != 1 || json.Unmarshal(contributions[0].Descriptor, &signed) != nil || json.Unmarshal(Descriptor(), &runtime) != nil || !reflect.DeepEqual(signed, runtime) {
		t.Fatalf("运行时 descriptor 与签名 Manifest 不一致\nsigned=%s\nruntime=%s", contributions[0].Descriptor, Descriptor())
	}
}

type fakeTransit struct{ encrypts, rewraps int }

func (f *fakeTransit) Encrypt(_ context.Context, value []byte) (string, error) {
	f.encrypts++
	return "vault:v1:" + string(value), nil
}
func (f *fakeTransit) Rewrap(_ context.Context, cipher string) (string, error) {
	f.rewraps++
	return "vault:v2:" + strings.TrimPrefix(cipher, "vault:v1:"), nil
}
func credentialContext(tenant string) *contractv1.CallContext {
	return &contractv1.CallContext{TenantId: tenant}
}

func managedContext(tenant, pluginID string) *contractv1.CallContext {
	return &contractv1.CallContext{TenantId: tenant, Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: pluginID}}
}

func TestEnvelopeCredentialNeverReturnsCiphertextOrPlaintext(t *testing.T) {
	transit := &fakeTransit{}
	path := filepath.Join(t.TempDir(), "credentials.json")
	service, err := New(path, transit)
	if err != nil {
		t.Fatal(err)
	}
	call := credentialContext("tenant-a")
	record, err := service.Put(context.Background(), call, "db.password", "correct-horse")
	if err != nil || record.Ciphertext == "" || transit.encrypts != 1 {
		t.Fatalf("加密保存失败: record=%+v err=%v", record, err)
	}
	reopened, err := New(path, transit)
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := reopened.Describe(call, "db.password")
	if err != nil || persisted.Ciphertext == "" {
		t.Fatalf("密文未持久化: record=%+v err=%v", persisted, err)
	}
	result, payload, err := reopened.Handler(context.Background(), nil, call, []byte(`{"name":"db.password"}`), "describe")
	if err != nil || result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("describe 失败: %v", err)
	}
	if strings.Contains(string(payload), "correct-horse") || strings.Contains(string(payload), "vault:v1") || !json.Valid(payload) {
		t.Fatalf("协议响应泄露凭证: %s", payload)
	}
	if _, err := reopened.Rotate(context.Background(), call, "db.password"); err != nil || transit.rewraps != 1 {
		t.Fatalf("rewrap 轮换失败: %v", err)
	}
	if _, err := reopened.Revoke(call, "db.password"); err != nil {
		t.Fatal(err)
	}
}

func TestManagedCredentialIsOwnedByCallingPluginAndNeverExposesCiphertext(t *testing.T) {
	service, err := New(filepath.Join(t.TempDir(), "credentials.json"), &fakeTransit{})
	if err != nil {
		t.Fatal(err)
	}
	owner := managedContext("tenant-a", "plugin.database")
	result, raw, err := service.Handler(context.Background(), nil, owner, []byte(`{"purpose":"database.connection","resource":"primary","value":"db-secret"}`), "stageManaged")
	if err != nil || result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("stageManaged 失败: result=%+v err=%v", result, err)
	}
	if strings.Contains(string(raw), "db-secret") || strings.Contains(string(raw), "vault:") {
		t.Fatalf("托管凭证响应泄露 material: %s", raw)
	}
	var staged struct {
		ID  string `json:"stageId"`
		Ref struct {
			Handle string `json:"handle"`
			Owner  string `json:"owner"`
		} `json:"ref"`
	}
	if err := json.Unmarshal(raw, &staged); err != nil || staged.ID == "" || staged.Ref.Handle == "" || staged.Ref.Owner != "plugin.database" {
		t.Fatalf("托管凭证引用无效: raw=%s err=%v", raw, err)
	}
	if _, err := service.ActivateManaged(managedContext("tenant-a", "plugin.other"), staged.ID); err == nil {
		t.Fatal("其他插件不得激活托管凭证")
	}
	ref, err := service.ActivateManaged(owner, staged.ID)
	if err != nil || ref.Handle != staged.Ref.Handle {
		t.Fatalf("所有者激活失败: ref=%+v err=%v", ref, err)
	}
	if _, err := service.RetireManaged(owner, ref.Handle); err != nil {
		t.Fatalf("退役失败: %v", err)
	}
	if _, err := service.RetireManaged(owner, ref.Handle); err != nil {
		t.Fatalf("重复退役必须幂等以支持 Saga 恢复: %v", err)
	}
}
