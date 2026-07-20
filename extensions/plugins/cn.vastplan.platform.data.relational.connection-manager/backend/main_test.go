package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
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
	if len(contributions) != 1 || json.Unmarshal(contributions[0].Descriptor, &signed) != nil || json.Unmarshal(descriptor(), &runtime) != nil || !reflect.DeepEqual(signed, runtime) {
		t.Fatalf("运行时 descriptor 与签名 Manifest 不一致\nsigned=%s\nruntime=%s", contributions[0].Descriptor, descriptor())
	}
}

type probeHost struct {
	payload          []byte
	activated        int
	runtimeActivated int
	failActivations  int
	failRuntime      int
}

var _ sdk.Host = (*probeHost)(nil)

func (h *probeHost) Call(_ context.Context, target *contractv1.CallTarget, _ *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
	if target.GetCapability() == credentialCapability {
		switch target.GetOperation() {
		case "stageManaged":
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, []byte(`{"stageId":"stage-1","ref":{"handle":"credential://managed/opaque-1","scope":"tenant","owner":"cn.vastplan.platform.data.relational.connection-manager","purpose":"database.connection","version":1}}`), nil
		case "activateManaged":
			if h.failActivations > 0 {
				h.failActivations--
				return nil, nil, errors.New("credential service temporarily unavailable")
			}
			h.activated++
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, []byte(`{}`), nil
		case "retireManaged", "abortManaged":
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, []byte(`{}`), nil
		}
	}
	if target.GetCapability() == databasev1.Capability {
		h.payload = append([]byte(nil), payload...)
		switch target.GetOperation() {
		case databasev1.OperationActivate:
			if h.failRuntime > 0 {
				h.failRuntime--
				return nil, nil, errors.New("runtime temporarily unavailable")
			}
			h.runtimeActivated++
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, []byte(`{"connection":{"resourceId":"connection-test","revision":1},"generation":1,"ready":true}`), nil
		case databasev1.OperationProbe:
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, []byte(`{"ready":true,"providerId":"postgresql","latencyMs":1}`), nil
		case databasev1.OperationRetire:
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, []byte(`{}`), nil
		}
	}
	return nil, nil, errors.New("unexpected capability or operation")
}

func TestRuntimePublicationOutboxRecoversWithoutLosingDefinition(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connections.json")
	service, err := newService(path)
	if err != nil {
		t.Fatal(err)
	}
	host := &probeHost{failRuntime: 1}
	_, raw, err := service.handle(context.Background(), host, dbContext(), []byte(`{"name":"primary","providerId":"postgresql","endpoint":"db.internal:5432","options":{"user":"app"},"credentialValue":"correct-horse"}`), "define")
	if err != nil || !strings.Contains(string(raw), `"runtime":"pending"`) {
		t.Fatalf("Runtime 暂不可用时应保存期望定义并报告 pending: raw=%s err=%v", raw, err)
	}
	reopened, err := newService(path)
	if err != nil {
		t.Fatal(err)
	}
	_, raw, err = reopened.handle(context.Background(), host, dbContext(), []byte(`{}`), "list")
	if err != nil || !strings.Contains(string(raw), `"runtime":"ready"`) || host.runtimeActivated != 1 {
		t.Fatalf("publication outbox 未在重启后收敛: raw=%s runtime=%d err=%v", raw, host.runtimeActivated, err)
	}
}

func TestDeleteAndRecreateKeepsConnectionRevisionMonotonic(t *testing.T) {
	service, err := newService(filepath.Join(t.TempDir(), "connections.json"))
	if err != nil {
		t.Fatal(err)
	}
	host := &probeHost{}
	define := []byte(`{"name":"primary","providerId":"postgresql","endpoint":"db.internal:5432","options":{"user":"app"},"credentialValue":"correct-horse"}`)
	if _, raw, err := service.handle(context.Background(), host, dbContext(), define, "define"); err != nil || !strings.Contains(string(raw), `"revision":1`) {
		t.Fatalf("首次定义失败: raw=%s err=%v", raw, err)
	}
	if _, _, err := service.handle(context.Background(), host, dbContext(), []byte(`{"name":"primary"}`), "remove"); err != nil {
		t.Fatal(err)
	}
	if _, raw, err := service.handle(context.Background(), host, dbContext(), define, "define"); err != nil || !strings.Contains(string(raw), `"revision":2`) {
		t.Fatalf("同名重建不得回退 revision: raw=%s err=%v", raw, err)
	}
}

func TestPendingCredentialActivationRecoversAfterRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connections.json")
	service, err := newService(path)
	if err != nil {
		t.Fatal(err)
	}
	host := &probeHost{failActivations: 1}
	if _, _, err := service.handle(context.Background(), host, dbContext(), []byte(`{"name":"primary","providerId":"postgresql","endpoint":"db.internal:5432","options":{"user":"app"},"credentialValue":"correct-horse"}`), "define"); err == nil {
		t.Fatal("第一次激活失败应返回错误并保留 pending")
	}
	reopened, err := newService(path)
	if err != nil {
		t.Fatal(err)
	}
	result, raw, err := reopened.handle(context.Background(), host, dbContext(), []byte(`{}`), "list")
	if err != nil || result.GetStatus() != contractv1.CallResult_STATUS_OK || !strings.Contains(string(raw), `"credential":{"managed":true,"version":1}`) || strings.Contains(string(raw), "credential://managed/") || host.activated != 1 || host.runtimeActivated != 1 {
		t.Fatalf("重启后未收敛 pending: raw=%s activated=%d runtime=%d err=%v", raw, host.activated, host.runtimeActivated, err)
	}
}
func dbContext() *contractv1.CallContext {
	return &contractv1.CallContext{TenantId: "tenant-a", Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_USER, Id: "admin"}}
}
func TestConnectionDefinitionPersistsAndProbeHasNoSecret(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connections.json")
	s, err := newService(path)
	if err != nil {
		t.Fatal(err)
	}
	host := &probeHost{}
	_, response, err := s.handle(context.Background(), host, dbContext(), []byte(`{"name":"primary","providerId":"postgresql","endpoint":"db.internal:5432","database":"app","options":{"user":"app"},"credentialValue":"correct-horse"}`), "define")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(response), "correct-horse") || strings.Contains(string(response), "credential://managed/") || host.activated != 1 || host.runtimeActivated != 1 {
		t.Fatalf("响应泄露明文或候选未激活: response=%s activated=%d", response, host.activated)
	}
	reopened, err := newService(path)
	if err != nil {
		t.Fatal(err)
	}
	_, raw, err := reopened.handle(context.Background(), host, dbContext(), []byte(`{"name":"primary"}`), "probe")
	if err != nil || !strings.Contains(string(raw), `"ready":true`) || !strings.Contains(string(raw), `"providerId":"postgresql"`) {
		t.Fatalf("probe failed raw=%s err=%v", raw, err)
	}
	if strings.Contains(string(host.payload), "password") || strings.Contains(string(host.payload), "secret") {
		t.Fatalf("probe payload leaked secret: %s", host.payload)
	}
	var request map[string]any
	if err := json.Unmarshal(host.payload, &request); err != nil {
		t.Fatal(err)
	}
	connection, ok := request["connection"].(map[string]any)
	if !ok || connection["credentials"].(map[string]any)["handle"] != "credential://managed/opaque-1" {
		t.Fatalf("credential reference missing: %s", host.payload)
	}
}
