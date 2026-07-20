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
	payload         []byte
	activated       int
	failActivations int
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
	if target.GetCapability() != "kernel.database.probe" {
		return nil, nil, errors.New("unexpected capability or operation")
	}
	h.payload = append([]byte(nil), payload...)
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, []byte(`{"ready":true}`), nil
}

func TestPendingCredentialActivationRecoversAfterRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connections.json")
	service, err := newService(path)
	if err != nil {
		t.Fatal(err)
	}
	host := &probeHost{failActivations: 1}
	if _, _, err := service.handle(context.Background(), host, dbContext(), []byte(`{"name":"primary","driver":"postgres","endpoint":"db.internal:5432","credentialValue":"correct-horse"}`), "define"); err == nil {
		t.Fatal("第一次激活失败应返回错误并保留 pending")
	}
	reopened, err := newService(path)
	if err != nil {
		t.Fatal(err)
	}
	result, raw, err := reopened.handle(context.Background(), host, dbContext(), []byte(`{}`), "list")
	if err != nil || result.GetStatus() != contractv1.CallResult_STATUS_OK || !strings.Contains(string(raw), `"credential":{"managed":true,"version":1}`) || strings.Contains(string(raw), "credential://managed/") || host.activated != 1 {
		t.Fatalf("重启后未收敛 pending: raw=%s activated=%d err=%v", raw, host.activated, err)
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
	_, response, err := s.handle(context.Background(), host, dbContext(), []byte(`{"name":"primary","driver":"postgres","endpoint":"db.internal:5432","database":"app","credentialValue":"correct-horse"}`), "define")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(response), "correct-horse") || strings.Contains(string(response), "credential://managed/") || host.activated != 1 {
		t.Fatalf("响应泄露明文或候选未激活: response=%s activated=%d", response, host.activated)
	}
	reopened, err := newService(path)
	if err != nil {
		t.Fatal(err)
	}
	_, raw, err := reopened.handle(context.Background(), host, dbContext(), []byte(`{"name":"primary"}`), "probe")
	if err != nil || string(raw) != "{\"ready\":true}" {
		t.Fatalf("probe failed raw=%s err=%v", raw, err)
	}
	if strings.Contains(string(host.payload), "password") || strings.Contains(string(host.payload), "secret") {
		t.Fatalf("probe payload leaked secret: %s", host.payload)
	}
	var request map[string]any
	if err := json.Unmarshal(host.payload, &request); err != nil {
		t.Fatal(err)
	}
	if request["credentials"].(map[string]any)["handle"] != "credential://managed/opaque-1" {
		t.Fatalf("credential reference missing: %s", host.payload)
	}
}
