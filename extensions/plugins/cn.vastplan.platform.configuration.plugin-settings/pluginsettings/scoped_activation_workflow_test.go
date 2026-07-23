package pluginsettings

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
	configurationscopedv1 "cdsoft.com.cn/VastPlan/contracts/schemas/configurationscoped/v1"
	deploymentv1 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v1"
	deploymentv2 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v2"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfiguration"
)

func TestTenantScopedCandidateBecomesAtomicActiveAndSurvivesRestart(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "plugin-settings.json")
	service, _ := newTestService(stateFile)
	host, definition := scopedFixture(t, "tenant")
	alice, bob := userCall("tenant-a", "alice"), userCall("tenant-a", "bob")
	draft, err := service.CreateDraft(context.Background(), host, alice, pluginconfiguration.CreateDraftRequest{
		ConfigurationID: definition.ID, CatalogDigest: host.catalogs[0].Digest, Values: scopedValues("Welcome {{name}}"),
	})
	if err != nil || draft.ApplyPath != pluginconfiguration.ApplyHotScoped || draft.ExternalRevision != 0 || draft.ExternalDigest == "" {
		t.Fatalf("创建 tenant scoped 草稿: %+v err=%v", draft, err)
	}
	pending, err := service.SubmitScopedDraft(context.Background(), host, alice, draft.ID, draft.Revision)
	if err != nil || pending.ExternalStatus != "PendingApproval" {
		t.Fatalf("提交 scoped 草稿: %+v err=%v", pending, err)
	}
	if _, err := service.ApproveScopedCandidate(alice, pending.ID, pending.Revision); err == nil {
		t.Fatal("创建者不得自批")
	}
	service, err = newTestService(stateFile)
	if err != nil {
		t.Fatal(err)
	}
	approved, err := service.ApproveScopedCandidate(bob, pending.ID, pending.Revision)
	if err != nil || approved.ExternalStatus != "Approved" {
		t.Fatalf("批准 scoped 候选: %+v err=%v", approved, err)
	}
	ready, err := service.ActivateScopedCandidate(context.Background(), host, bob, approved.ID, approved.Revision)
	if err != nil || ready.Status != pluginconfiguration.CandidateReady || ready.ExternalRevision != 1 || ready.ExternalDigest == "" {
		t.Fatalf("激活 scoped 候选: %+v err=%v", ready, err)
	}

	reopened, err := newTestService(stateFile)
	if err != nil {
		t.Fatal(err)
	}
	resolution := resolveScopedForTest(t, reopened, host, pluginCall("tenant-a", definition.PluginID, "subject-a"))
	if resolution.Source != "active" || resolution.Revision != 1 || string(resolution.Values) != string(scopedValues("Welcome {{name}}")) {
		t.Fatalf("重启后 scoped Active 丢失: %+v", resolution)
	}
	seed := resolveScopedForTest(t, reopened, host, pluginCall("tenant-b", definition.PluginID, "subject-a"))
	if seed.Source != "seed" || seed.Revision != 0 {
		t.Fatalf("跨 tenant 不得读取 Active: %+v", seed)
	}
}

func TestUserScopedResolutionUsesTrustedPrincipalAndWatchIsValueFree(t *testing.T) {
	service, _ := newTestService(filepath.Join(t.TempDir(), "plugin-settings.json"))
	host, definition := scopedFixture(t, "user")
	alice, bob := userCall("tenant-a", "alice"), userCall("tenant-a", "bob")
	draft, err := service.CreateDraft(context.Background(), host, alice, pluginconfiguration.CreateDraftRequest{
		ConfigurationID: definition.ID, CatalogDigest: host.catalogs[0].Digest, ScopeSubjectID: "subject-a", Values: scopedValues("User {{name}}"),
	})
	if err != nil || draft.ScopeSubjectID != "subject-a" {
		t.Fatalf("创建 user scoped 草稿: %+v err=%v", draft, err)
	}
	pending, _ := service.SubmitScopedDraft(context.Background(), host, alice, draft.ID, draft.Revision)
	approved, _ := service.ApproveScopedCandidate(bob, pending.ID, pending.Revision)
	if _, err := service.ActivateScopedCandidate(context.Background(), host, bob, approved.ID, approved.Revision); err != nil {
		t.Fatal(err)
	}
	active := resolveScopedForTest(t, service, host, pluginCall("tenant-a", definition.PluginID, "subject-a"))
	seed := resolveScopedForTest(t, service, host, pluginCall("tenant-a", definition.PluginID, "subject-b"))
	if active.Source != "active" || seed.Source != "seed" {
		t.Fatalf("user scope 隔离失败: active=%+v seed=%+v", active, seed)
	}
	request, _ := json.Marshal(configurationscopedv1.WatchRevisionRequest{AfterRevision: 0, AfterDigest: seed.Digest, TimeoutMS: 1})
	result, raw, err := service.handleScopedWatch(context.Background(), host, pluginCall("tenant-a", definition.PluginID, "subject-a"), request)
	if err != nil || result.Status != contractv1.CallResult_STATUS_OK {
		t.Fatalf("watch revision: result=%+v err=%v", result, err)
	}
	if strings.Contains(string(raw), "values") || strings.Contains(string(raw), "subject-a") || strings.Contains(string(raw), "tenant-a") {
		t.Fatalf("watch 泄漏 scope 或 values: %s", raw)
	}
	denied, _, _ := service.handleScopedResolve(context.Background(), host, alice, []byte(`{}`))
	if denied.GetError().GetCode() != "configuration.scoped.denied" {
		t.Fatalf("用户不得直接调用 resolver: %+v", denied)
	}
}

func resolveScopedForTest(t *testing.T, service *Service, host *catalogHost, call *contractv1.CallContext) configurationscopedv1.Resolution {
	t.Helper()
	result, raw, err := service.handleScopedResolve(context.Background(), host, call, []byte(`{}`))
	if err != nil || result.Status != contractv1.CallResult_STATUS_OK {
		t.Fatalf("resolve scoped: result=%+v err=%v", result, err)
	}
	var response configurationscopedv1.Resolution
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatal(err)
	}
	return response
}

func scopedFixture(t *testing.T, scope string) (*catalogHost, pluginconfiguration.Definition) {
	t.Helper()
	pluginID := "cn.example.scoped-" + scope
	manifest := []byte(fmt.Sprintf(`{
		"id":%q,"name":"Scoped","description":"Scoped config test","version":"1.0.0","publisher":"example","engines":{"backend":"^0.1"},
		"runtime":{"instancePolicy":"active-active","stateModel":"external-shared","visibility":"cluster","routing":"queue","routingDomain":"application","requires":[{"capability":"configuration.scoped","scope":"remote","kind":"strong","ready":"readiness","failurePolicy":"fail"}]},
		"configuration":{"scope":%q,"applyMode":"hot","schema":{"type":"object","additionalProperties":false,"required":["greetingTemplate"],"properties":{"greetingTemplate":{"type":"string"}}}},
		"activation":["onStartup"],"entry":{"backend":"backend/main"},"contributes":{"backend":{"tools":[{"id":"test.scoped","service_role":"backend","title":"Test","subcommands":[{"name":"run","description":"run"}]}]}}
	}`, pluginID, scope))
	ref := pluginv1.ArtifactRef{PluginID: pluginID, Version: "1.0.0", Channel: "stable"}
	var values map[string]any
	_ = json.Unmarshal(scopedValues("Seed {{name}}"), &values)
	deployment := deploymentv2.Deployment{
		Version: 2, Revision: 1, Metadata: deploymentv1.Metadata{Name: "scoped-services", Tenant: "tenant-a"},
		Resolution: deploymentv2.Resolution{PlatformProfile: compositioncommonv1.Ref{ID: "platform", Revision: 1, Digest: strings.Repeat("1", 64)}, PluginOrigins: map[string]string{pluginID: deploymentv2.OriginApplication}},
		Units: []deploymentv2.ServiceUnit{{ID: "scoped", Kind: "service", Enabled: true, ServiceRole: "backend", Replicas: 1,
			Plugins: []deploymentv1.PluginRef{{ID: pluginID, Version: "1.0.0", Channel: "stable"}}, Config: map[string]any{"plugins": map[string]any{pluginID: values}}}},
	}
	catalog, err := pluginconfiguration.Build(deployment, map[pluginv1.ArtifactRef]pluginv1.Artifact{ref: {PluginID: pluginID, Version: "1.0.0", Channel: "stable", SHA256: strings.Repeat("a", 64), Manifest: manifest}})
	if err != nil {
		t.Fatal(err)
	}
	return &catalogHost{catalogs: []pluginconfiguration.Catalog{catalog}}, catalog.Items[0]
}

func scopedValues(template string) json.RawMessage {
	raw, _ := json.Marshal(map[string]string{"greetingTemplate": template})
	return raw
}

func pluginCall(tenant, pluginID, subject string) *contractv1.CallContext {
	return &contractv1.CallContext{TenantId: tenant, Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: pluginID}, Principal: &contractv1.Principal{UserId: subject}}
}
