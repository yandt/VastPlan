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

type probeHost struct{ payload []byte }

var _ sdk.Host = (*probeHost)(nil)

func (h *probeHost) Call(_ context.Context, target *contractv1.CallTarget, _ *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
	if target.GetCapability() != "kernel.database.probe" {
		return nil, nil, errors.New("unexpected capability")
	}
	h.payload = append([]byte(nil), payload...)
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, []byte(`{"ready":true}`), nil
}
func dbContext() *contractv1.CallContext { return &contractv1.CallContext{TenantId: "tenant-a"} }
func TestConnectionDefinitionPersistsAndProbeHasNoSecret(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connections.json")
	s, err := newService(path)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = s.handle(context.Background(), nil, dbContext(), []byte(`{"name":"primary","driver":"postgres","endpoint":"db.internal:5432","database":"app","credential":"postgres-main"}`), "define")
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := newService(path)
	if err != nil {
		t.Fatal(err)
	}
	host := &probeHost{}
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
	if request["credentials"].(map[string]any)["name"] != "postgres-main" {
		t.Fatalf("credential reference missing: %s", host.payload)
	}
}
