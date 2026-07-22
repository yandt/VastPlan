package pluginsettings

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	deploymentv1 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v1"
	deploymentv2 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v2"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfiguration"
)

type catalogHost struct {
	catalogs []pluginconfiguration.Catalog
	targets  []*contractv1.CallTarget
}

func (h *catalogHost) Call(_ context.Context, target *contractv1.CallTarget, _ *contractv1.CallContext, _ []byte) (*contractv1.CallResult, []byte, error) {
	h.targets = append(h.targets, target)
	if target.GetExtensionPoint() != "kernel.service" || target.GetCapability() != pluginconfiguration.KernelCatalogsService || target.GetOperation() != "list" || target.LogicalService != nil || target.RoutingDomain != nil {
		return nil, nil, fmt.Errorf("unexpected target: %+v", target)
	}
	raw, _ := json.Marshal(map[string]any{"items": h.catalogs})
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
}

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

func TestDraftIsSchemaValidatedCASBoundAndDurable(t *testing.T) {
	catalog := testCatalog(t)
	host := &catalogHost{catalogs: []pluginconfiguration.Catalog{catalog}}
	stateFile := filepath.Join(t.TempDir(), "plugin-settings.json")
	service, err := New(stateFile)
	if err != nil {
		t.Fatal(err)
	}
	call := userCall("tenant-a", "alice")
	definition := catalog.Items[0]
	candidate, err := service.CreateDraft(context.Background(), host, call, pluginconfiguration.CreateDraftRequest{
		ConfigurationID: definition.ID, CatalogDigest: catalog.Digest, Values: []byte(`{"region":"cn-east"}`),
	})
	if err != nil || candidate.Status != pluginconfiguration.CandidateDraft || candidate.Revision != 1 || candidate.CreatedBy != "alice" {
		t.Fatalf("创建配置草稿失败: candidate=%+v err=%v", candidate, err)
	}
	if _, err := service.CreateDraft(context.Background(), host, call, pluginconfiguration.CreateDraftRequest{ConfigurationID: definition.ID, CatalogDigest: catalog.Digest, Values: []byte(`{"region":"cn-west"}`)}); !errorsIs(err, ErrConflict) {
		t.Fatalf("同一配置存在未完成候选时必须冲突: %v", err)
	}
	if _, err := service.CreateDraft(context.Background(), host, call, pluginconfiguration.CreateDraftRequest{ConfigurationID: definition.ID, CatalogDigest: strings.Repeat("f", 64), Values: []byte(`{"region":"cn-west"}`)}); !errorsIs(err, ErrConflict) {
		t.Fatalf("过期目录摘要必须冲突: %v", err)
	}
	if _, err := service.CreateDraft(context.Background(), host, call, pluginconfiguration.CreateDraftRequest{ConfigurationID: definition.ID, CatalogDigest: catalog.Digest, Values: []byte(`{"region":"x","token":"secret"}`)}); !errorsIs(err, ErrInvalid) {
		t.Fatalf("违反签名 Schema 的值必须拒绝: %v", err)
	}
	if _, err := service.DiscardDraft(call, candidate.ID, 9); !errorsIs(err, ErrConflict) {
		t.Fatalf("错误 CAS revision 必须冲突: %v", err)
	}
	discarded, err := service.DiscardDraft(call, candidate.ID, candidate.Revision)
	if err != nil || discarded.Status != pluginconfiguration.CandidateRolledBack || discarded.Revision != 2 {
		t.Fatalf("放弃草稿失败: candidate=%+v err=%v", discarded, err)
	}
	info, err := os.Stat(stateFile)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("状态文件权限错误: info=%v err=%v", info, err)
	}
	reopened, err := New(stateFile)
	if err != nil {
		t.Fatal(err)
	}
	items, err := reopened.ListCandidates(call)
	if err != nil || len(items) != 1 || items[0].Status != pluginconfiguration.CandidateRolledBack {
		t.Fatalf("候选未跨重启恢复: items=%+v err=%v", items, err)
	}
}

func TestCatalogTamperingFailsClosed(t *testing.T) {
	catalog := testCatalog(t)
	catalog.Items[0].PluginName = "tampered"
	service, err := New(filepath.Join(t.TempDir(), "plugin-settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.catalogs(context.Background(), &catalogHost{catalogs: []pluginconfiguration.Catalog{catalog}}, userCall("tenant-a", "alice"))
	if err == nil {
		t.Fatal("篡改配置目录必须 fail-closed")
	}
}

func testCatalog(t *testing.T) pluginconfiguration.Catalog {
	t.Helper()
	const pluginID = "com.example.configured"
	manifest := []byte(fmt.Sprintf(`{"id":%q,"name":"Configured","description":"configured","version":"1.0.0","publisher":"example","engines":{"backend":"^1.0"},"configuration":{"scope":"service","applyMode":"restart","schema":{"type":"object","additionalProperties":false,"required":["region"],"properties":{"region":{"type":"string","minLength":2}}}},"activation":["onStartup"],"entry":{"backend":"backend/main"},"contributes":{"backend":{"tools":[]}}}`, pluginID))
	ref := pluginv1.ArtifactRef{PluginID: pluginID, Version: "1.0.0", Channel: "stable"}
	deployment := deploymentv2.Deployment{
		Version: 2, Revision: 7, Metadata: deploymentv1.Metadata{Name: "agent-services", Tenant: "tenant-a"},
		Resolution: deploymentv2.Resolution{PluginOrigins: map[string]string{pluginID: deploymentv2.OriginApplication}},
		Units: []deploymentv2.ServiceUnit{{ID: "api", Kind: "service", Enabled: true, ServiceRole: "backend", Replicas: 1,
			Plugins: []deploymentv1.PluginRef{{ID: pluginID, Version: "1.0.0", Channel: "stable"}},
			Config:  map[string]any{"plugins": map[string]any{pluginID: map[string]any{"region": "cn-east"}}},
		}},
	}
	catalog, err := pluginconfiguration.Build(deployment, map[pluginv1.ArtifactRef]pluginv1.Artifact{ref: {PluginID: pluginID, Version: "1.0.0", Channel: "stable", SHA256: strings.Repeat("a", 64), Manifest: manifest}})
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}

func userCall(tenant, user string) *contractv1.CallContext {
	return &contractv1.CallContext{TenantId: tenant, Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_USER, Id: user}, Principal: &contractv1.Principal{UserId: user}}
}

func errorsIs(err, target error) bool {
	return err != nil && strings.Contains(err.Error(), target.Error())
}
