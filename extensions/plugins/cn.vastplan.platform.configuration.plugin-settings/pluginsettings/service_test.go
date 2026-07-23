package pluginsettings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	deploymentv1 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v1"
	deploymentv2 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v2"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/configurationactivation"
	"cdsoft.com.cn/VastPlan/core/shared/go/configurationauthority"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfig"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfiguration"
)

type catalogHost struct {
	catalogs []pluginconfiguration.Catalog
	targets  []*contractv1.CallTarget
}

type credentialDraftHost struct {
	catalogHost
	definition                                          pluginconfiguration.Definition
	stageCalls, prepareCalls, activateCalls, abortCalls int
	activationStatus                                    configurationactivation.Status
}

func (h *credentialDraftHost) Call(ctx context.Context, target *contractv1.CallTarget, call *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
	if target.GetCapability() == pluginconfiguration.KernelCatalogsService {
		return h.catalogHost.Call(ctx, target, call, payload)
	}
	switch target.GetCapability() {
	case configurationauthority.KernelIssueService:
		if target.GetExtensionPoint() != extpoint.KernelService || target.GetOperation() != "issue" {
			return nil, nil, fmt.Errorf("unexpected authority target: %+v", target)
		}
		var request configurationauthority.IssueRequest
		_ = json.Unmarshal(payload, &request)
		if request.ConfigurationID != h.definition.ID || request.FieldID != "token" {
			return nil, nil, fmt.Errorf("unexpected authority request: %+v", request)
		}
		raw, _ := json.Marshal(configurationauthority.Issued{Token: configurationauthority.TokenPrefix + strings.Repeat("1", 64), ExpiresAt: time.Now().UTC().Add(time.Minute)})
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
	case credentialCapability:
		if target.GetExtensionPoint() != extpoint.ToolPackage || target.GetLogicalService() != "platform.credentials" || target.GetRoutingDomain() != "platform" {
			return nil, nil, fmt.Errorf("unexpected credential target: %+v", target)
		}
		switch target.GetOperation() {
		case "stageDelegated":
			h.stageCalls++
			raw, _ := json.Marshal(pluginconfig.StagedCredential{ID: "stage-" + strings.Repeat("2", 32), Ref: pluginconfig.ManagedCredentialRef{Handle: "credential://managed/" + strings.Repeat("3", 32), Scope: "tenant", Owner: h.definition.PluginID, Purpose: "remote.token", Version: 1}})
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
		case "abortDelegated":
			h.abortCalls++
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, []byte(`{}`), nil
		case "prepareDelegated":
			h.prepareCalls++
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, []byte(`{}`), nil
		case "activateDelegated":
			h.activateCalls++
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, []byte(`{}`), nil
		}
	case configurationactivation.DeploymentCapability:
		status := h.activationStatus
		if status == "" {
			status = configurationactivation.StatusPendingApproval
		}
		activation := configurationactivation.Activation{CandidateID: "pcfg_" + strings.Repeat("9", 32), ConfigurationID: h.definition.ID, Deployment: h.definition.Deployment, ServiceRevision: 9, PreviousServiceRevision: h.definition.DeploymentRevision, Status: status}
		var request map[string]json.RawMessage
		_ = json.Unmarshal(payload, &request)
		if rawCandidate := request["candidateId"]; len(rawCandidate) > 0 {
			_ = json.Unmarshal(rawCandidate, &activation.CandidateID)
		}
		if rawActivation := request["activation"]; len(rawActivation) > 0 {
			var create configurationactivation.CreateRequest
			_ = json.Unmarshal(rawActivation, &create)
			activation.CandidateID, activation.ConfigurationID = create.CandidateID, create.ConfigurationID
		}
		if target.GetOperation() == configurationactivation.PublishOperation && status == configurationactivation.StatusApproved {
			activation.Status = configurationactivation.StatusReady
		}
		raw, _ := json.Marshal(activation)
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
	}
	return nil, nil, fmt.Errorf("unexpected target: %+v", target)
}

func TestApplicationDraftSubmissionApprovalAndActivationSaga(t *testing.T) {
	catalog := managedTestCatalog(t)
	host := &credentialDraftHost{catalogHost: catalogHost{catalogs: []pluginconfiguration.Catalog{catalog}}, definition: catalog.Items[0]}
	service, err := New(filepath.Join(t.TempDir(), "plugin-settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	call := userCall("tenant-a", "alice")
	draft, err := service.CreateDraft(context.Background(), host, call, pluginconfiguration.CreateDraftRequest{ConfigurationID: catalog.Items[0].ID, CatalogDigest: catalog.Digest, Values: []byte(`{"region":"cn-west"}`), Secrets: map[string]string{"token": "secret"}})
	if err != nil {
		t.Fatal(err)
	}
	submitted, err := service.SubmitDraft(context.Background(), host, call, draft.ID, draft.Revision)
	if err != nil || submitted.Status != pluginconfiguration.CandidatePublishing || submitted.ExternalRevision != 9 || submitted.ExternalStatus != string(configurationactivation.StatusPendingApproval) {
		t.Fatalf("配置草稿未提交外部审批: candidate=%+v err=%v", submitted, err)
	}
	host.activationStatus = configurationactivation.StatusApproved
	if err := service.recoverInterrupted(context.Background(), host, call); err != nil {
		t.Fatal(err)
	}
	items, _ := service.ListCandidates(call)
	approved := items[0]
	if approved.ExternalStatus != string(configurationactivation.StatusApproved) {
		t.Fatalf("外部审批状态未恢复: %+v", approved)
	}
	ready, err := service.ActivateCandidate(context.Background(), host, call, approved.ID, approved.Revision)
	if err != nil || ready.Status != pluginconfiguration.CandidateReady || ready.ExternalStatus != string(configurationactivation.StatusReady) || host.prepareCalls != 1 || host.activateCalls != 1 {
		t.Fatalf("配置候选未完成激活 Saga: candidate=%+v prepare=%d activate=%d err=%v", ready, host.prepareCalls, host.activateCalls, err)
	}
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
	if _, err := service.CreateDraft(context.Background(), host, call, pluginconfiguration.CreateDraftRequest{ConfigurationID: definition.ID, CatalogDigest: catalog.Digest, Values: []byte(`{"region":"cn-west"}`)}); !errors.Is(err, ErrConflict) {
		t.Fatalf("同一配置存在未完成候选时必须冲突: %v", err)
	}
	if _, err := service.CreateDraft(context.Background(), host, call, pluginconfiguration.CreateDraftRequest{ConfigurationID: definition.ID, CatalogDigest: strings.Repeat("f", 64), Values: []byte(`{"region":"cn-west"}`)}); !errors.Is(err, ErrConflict) {
		t.Fatalf("过期目录摘要必须冲突: %v", err)
	}
	if _, err := service.CreateDraft(context.Background(), host, call, pluginconfiguration.CreateDraftRequest{ConfigurationID: definition.ID, CatalogDigest: catalog.Digest, Values: []byte(`{"region":"x","token":"secret"}`)}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("违反签名 Schema 的值必须拒绝: %v", err)
	}
	if _, err := service.DiscardDraft(context.Background(), nil, call, candidate.ID, 9); !errors.Is(err, ErrConflict) {
		t.Fatalf("错误 CAS revision 必须冲突: %v", err)
	}
	discarded, err := service.DiscardDraft(context.Background(), nil, call, candidate.ID, candidate.Revision)
	if err != nil || discarded.Status != pluginconfiguration.CandidateRolledBack || discarded.Revision != 3 {
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

func TestDraftStagesDeclaredSecretWithoutPersistingMaterialOrAuthority(t *testing.T) {
	catalog := managedTestCatalog(t)
	host := &credentialDraftHost{catalogHost: catalogHost{catalogs: []pluginconfiguration.Catalog{catalog}}, definition: catalog.Items[0]}
	stateFile := filepath.Join(t.TempDir(), "plugin-settings.json")
	service, err := New(stateFile)
	if err != nil {
		t.Fatal(err)
	}
	call := userCall("tenant-a", "alice")
	candidate, err := service.CreateDraft(context.Background(), host, call, pluginconfiguration.CreateDraftRequest{
		ConfigurationID: catalog.Items[0].ID, CatalogDigest: catalog.Digest, Values: []byte(`{"region":"cn-east"}`), Secrets: map[string]string{"token": "super-secret"},
	})
	if err != nil || candidate.Status != pluginconfiguration.CandidateDraft || candidate.Revision != 3 || host.stageCalls != 1 || len(candidate.ManagedCredentials) != 1 || !candidate.ManagedCredentials[0].Staged || candidate.ManagedCredentials[0].State != "Staged" {
		t.Fatalf("带秘密配置草稿未完成委托暂存: candidate=%+v stageCalls=%d err=%v", candidate, host.stageCalls, err)
	}
	raw, err := os.ReadFile(stateFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "super-secret") || strings.Contains(string(raw), configurationauthority.TokenPrefix) {
		t.Fatalf("协调器状态不得保存 material 或 authority: %s", raw)
	}
	discarded, err := service.DiscardDraft(context.Background(), host, call, candidate.ID, candidate.Revision)
	if err != nil || discarded.Status != pluginconfiguration.CandidateRolledBack || host.abortCalls != 1 {
		t.Fatalf("放弃带秘密草稿必须先终止委托凭证: candidate=%+v abortCalls=%d err=%v", discarded, host.abortCalls, err)
	}
}

func TestDraftRejectsMissingOrUndeclaredSecretBeforeAuthorityIssue(t *testing.T) {
	catalog := managedTestCatalog(t)
	host := &credentialDraftHost{catalogHost: catalogHost{catalogs: []pluginconfiguration.Catalog{catalog}}, definition: catalog.Items[0]}
	service, _ := New(filepath.Join(t.TempDir(), "plugin-settings.json"))
	base := pluginconfiguration.CreateDraftRequest{ConfigurationID: catalog.Items[0].ID, CatalogDigest: catalog.Digest, Values: []byte(`{"region":"cn-east"}`)}
	if _, err := service.CreateDraft(context.Background(), host, userCall("tenant-a", "alice"), base); !errors.Is(err, ErrInvalid) {
		t.Fatalf("缺少 required secret 必须拒绝: %v", err)
	}
	base.Secrets = map[string]string{"token": "secret", "forged": "value"}
	if _, err := service.CreateDraft(context.Background(), host, userCall("tenant-a", "alice"), base); !errors.Is(err, ErrInvalid) || host.stageCalls != 0 {
		t.Fatalf("未声明 secret 必须在签发前拒绝: calls=%d err=%v", host.stageCalls, err)
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

func managedTestCatalog(t *testing.T) pluginconfiguration.Catalog {
	t.Helper()
	const pluginID = "com.example.managed"
	manifest := []byte(fmt.Sprintf(`{"id":%q,"name":"Managed","description":"managed","version":"1.0.0","publisher":"example","engines":{"backend":"^1.0"},"capabilities":{"kernelServices":["kernel.config.credential-ref"]},"configuration":{"scope":"service","applyMode":"restart","schema":{"type":"object","additionalProperties":false,"required":["region"],"properties":{"region":{"type":"string"}}},"managedCredentials":[{"id":"token","title":"Token","purpose":"remote.token","required":true}]},"activation":["onStartup"],"entry":{"backend":"backend/main"},"contributes":{"backend":{"tools":[]}}}`, pluginID))
	ref := pluginv1.ArtifactRef{PluginID: pluginID, Version: "1.0.0", Channel: "stable"}
	deployment := deploymentv2.Deployment{Version: 2, Revision: 8, Metadata: deploymentv1.Metadata{Name: "managed-services", Tenant: "tenant-a"}, Resolution: deploymentv2.Resolution{PluginOrigins: map[string]string{pluginID: deploymentv2.OriginApplication}}, Units: []deploymentv2.ServiceUnit{{ID: "api", Kind: "service", Enabled: true, ServiceRole: "backend", Replicas: 1, Plugins: []deploymentv1.PluginRef{{ID: pluginID, Version: "1.0.0", Channel: "stable"}}, Config: map[string]any{"plugins": map[string]any{pluginID: map[string]any{"region": "cn-east"}}}}}}
	catalog, err := pluginconfiguration.Build(deployment, map[pluginv1.ArtifactRef]pluginv1.Artifact{ref: {PluginID: pluginID, Version: "1.0.0", Channel: "stable", SHA256: strings.Repeat("b", 64), Manifest: manifest}})
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}

func userCall(tenant, user string) *contractv1.CallContext {
	return &contractv1.CallContext{TenantId: tenant, Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_USER, Id: user}, Principal: &contractv1.Principal{UserId: user}}
}
