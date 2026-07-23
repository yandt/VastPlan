package credentials

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/credentiallease"
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
	runtimeDescriptors := map[string][]byte{Capability: Descriptor(), MaterialLeaseCapability: MaterialLeaseDescriptor()}
	if len(contributions) != len(runtimeDescriptors) {
		t.Fatalf("签名贡献数量错误: %+v", contributions)
	}
	for _, contribution := range contributions {
		var signed, runtime any
		rawRuntime, ok := runtimeDescriptors[contribution.ID]
		if !ok || json.Unmarshal(contribution.Descriptor, &signed) != nil || json.Unmarshal(rawRuntime, &runtime) != nil || !reflect.DeepEqual(signed, runtime) {
			t.Fatalf("%s 运行时 descriptor 与签名 Manifest 不一致\nsigned=%s\nruntime=%s", contribution.ID, contribution.Descriptor, rawRuntime)
		}
	}
}

func TestManifestKeepsCredentialsLeaderAndFencedUntilActiveActivePrerequisites(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "vastplan.plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := pluginv1.ParseManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Runtime == nil || manifest.Runtime.InstancePolicy != "leader" || manifest.Runtime.StateModel != "external-shared" || manifest.Runtime.Routing != "leader" {
		t.Fatalf("Credentials 在 durable command 与独立维护 owner 完成前必须保持 leader: %+v", manifest.Runtime)
	}
	services := map[string]bool{}
	for _, capability := range manifest.Capabilities.KernelServices {
		services[capability] = true
	}
	for _, required := range []string{"kernel.state.shared.get", "kernel.state.shared.list", "kernel.state.shared.fenced.create", "kernel.state.shared.fenced.update", "kernel.state.shared.fenced.delete"} {
		if !services[required] {
			t.Fatalf("Credentials 缺少 fenced Shared State 契约 %s", required)
		}
	}
	for _, forbidden := range []string{"kernel.state.shared.create", "kernel.state.shared.update", "kernel.state.shared.delete"} {
		if services[forbidden] {
			t.Fatalf("Credentials 不得以普通 mutation 绕过 leader fencing: %s", forbidden)
		}
	}
}

type fakeTransit struct{ encrypts, rewraps, decrypts int }

func (f *fakeTransit) Encrypt(_ context.Context, value []byte) (string, error) {
	f.encrypts++
	return "vault:v1:" + string(value), nil
}
func (f *fakeTransit) Rewrap(_ context.Context, cipher string) (string, error) {
	f.rewraps++
	return "vault:v2:" + strings.TrimPrefix(cipher, "vault:v1:"), nil
}
func (f *fakeTransit) Decrypt(_ context.Context, cipher string) ([]byte, error) {
	f.decrypts++
	parts := strings.SplitN(cipher, ":", 3)
	if len(parts) != 3 {
		return nil, errors.New("invalid fake ciphertext")
	}
	return []byte(parts[2]), nil
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
	service, err := openTestService(path, transit)
	if err != nil {
		t.Fatal(err)
	}
	call := credentialContext("tenant-a")
	record, err := service.Put(context.Background(), call, "db.password", "correct-horse")
	if err != nil || record.Ciphertext == "" || transit.encrypts != 1 {
		t.Fatalf("加密保存失败: record=%+v err=%v", record, err)
	}
	reopened, err := openTestService(path, transit)
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
	service, err := openTestService(filepath.Join(t.TempDir(), "credentials.json"), &fakeTransit{})
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

func TestMaterialLeaseOnlyOpensForTrustedAudienceAndNeverReturnsPlaintext(t *testing.T) {
	transit := &fakeTransit{}
	service, err := openTestService(filepath.Join(t.TempDir(), "credentials.json"), transit)
	if err != nil {
		t.Fatal(err)
	}
	owner := managedContext("tenant-a", "plugin.database")
	staged, err := service.StageManaged(context.Background(), owner, "database.connection", "primary", []byte("db-secret"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ActivateManaged(owner, staged.ID); err != nil {
		t.Fatal(err)
	}
	request, recipient, err := credentiallease.NewRequest(staged.Ref)
	if err != nil {
		t.Fatal(err)
	}
	kernel := &contractv1.CallContext{TenantId: "tenant-a", Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_SYSTEM, Id: "node-a"}}
	envelope, err := service.IssueMaterialLease(context.Background(), kernel, request)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(envelope)
	if strings.Contains(string(raw), "db-secret") || strings.Contains(string(raw), "vault:") {
		t.Fatalf("lease 响应泄露 material: %s", raw)
	}
	material, err := recipient.Open(envelope, credentiallease.Claims{TenantID: "tenant-a", Audience: "node-a", Ref: staged.Ref}, time.Now().UTC())
	if err != nil || string(material) != "db-secret" {
		t.Fatalf("可信宿主解封失败: material=%q err=%v", material, err)
	}
	for index := range material {
		material[index] = 0
	}
	request, _, _ = credentiallease.NewRequest(staged.Ref)
	if _, err := service.IssueMaterialLease(context.Background(), owner, request); err == nil {
		t.Fatal("普通插件不得申请 material lease")
	}
	decrypts := transit.decrypts
	request.RecipientPublicKey = "invalid"
	if _, err := service.IssueMaterialLease(context.Background(), kernel, request); err == nil || transit.decrypts != decrypts {
		t.Fatalf("非法接收公钥必须在 Vault decrypt 前拒绝: err=%v decrypts=%d", err, transit.decrypts)
	}
}
