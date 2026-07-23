package pluginsettings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
	configurationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/configuration/v1"
	deploymentv1 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v1"
	deploymentv2 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v2"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/configurationauthority"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfig"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfiguration"
	"cdsoft.com.cn/VastPlan/extensions/sdk/go/configurationcontroller"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

type hotWorkflowHost struct {
	catalog       pluginconfiguration.Catalog
	controller    sdk.Contribution
	controllerID  string
	definition    pluginconfiguration.Definition
	stageCalls    int
	prepareCalls  int
	activateCalls int
	abortCalls    int
	retired       []string
	failActivate  bool
}

type hotWorkflowController struct {
	configurationID   string
	active            configurationv1.ActiveReference
	activeCredentials map[string]pluginconfig.ManagedCredentialRef
	candidates        map[string]hotWorkflowControllerCandidate
}

type hotWorkflowControllerCandidate struct {
	request       configurationv1.PrepareRequest
	digest        string
	config        string
	status        configurationv1.CandidateStatus
	credentials   map[string]pluginconfig.ManagedCredentialRef
	retirePending []pluginconfig.ManagedCredentialRef
}

func (c *hotWorkflowController) Prepare(_ context.Context, _ sdk.Host, _ *contractv1.CallContext, request configurationv1.PrepareRequest) (configurationv1.Observation, error) {
	if request.ConfigurationID != c.configurationID || request.ExpectedActive != c.active {
		return configurationv1.Observation{}, fmt.Errorf("stale active reference")
	}
	digest, err := configurationv1.DigestPrepareRequest(request)
	if err != nil {
		return configurationv1.Observation{}, err
	}
	credentials := cloneCredentialRefs(c.activeCredentials)
	for fieldID, ref := range request.ManagedCredentials {
		credentials[fieldID] = ref
	}
	config, err := configurationv1.DigestConfiguration(request.Values, credentials)
	if err != nil {
		return configurationv1.Observation{}, err
	}
	if existing, ok := c.candidates[request.CandidateID]; ok {
		if existing.digest != digest {
			return configurationv1.Observation{}, fmt.Errorf("candidate digest conflict")
		}
		return c.observation(existing), nil
	}
	retirePending := make([]pluginconfig.ManagedCredentialRef, 0)
	for fieldID, previous := range c.activeCredentials {
		if current, ok := credentials[fieldID]; !ok || current.Handle != previous.Handle {
			retirePending = append(retirePending, previous)
		}
	}
	candidate := hotWorkflowControllerCandidate{request: request, digest: digest, config: config, status: configurationv1.StatusPrepared, credentials: credentials, retirePending: retirePending}
	c.candidates[request.CandidateID] = candidate
	return c.observation(candidate), nil
}

func (c *hotWorkflowController) Commit(ctx context.Context, host sdk.Host, call *contractv1.CallContext, request configurationv1.CandidateRequest) (configurationv1.Observation, error) {
	candidate, ok := c.candidates[request.CandidateID]
	if !ok || candidate.digest != request.RequestDigest {
		return configurationv1.Observation{}, fmt.Errorf("candidate not found")
	}
	if candidate.status == configurationv1.StatusAborted {
		return configurationv1.Observation{}, fmt.Errorf("candidate already aborted")
	}
	if candidate.status == configurationv1.StatusPrepared {
		c.active = configurationv1.ActiveReference{Revision: c.active.Revision + 1, Digest: candidate.config}
		c.activeCredentials = cloneCredentialRefs(candidate.credentials)
		candidate.status = configurationv1.StatusCommitted
		c.candidates[request.CandidateID] = candidate
	}
	for _, ref := range candidate.retirePending {
		operation, logicalService, routingDomain := "retireManaged", "platform.credentials", "platform"
		payload, _ := json.Marshal(map[string]string{"handle": ref.Handle})
		result, _, err := host.Call(ctx, &contractv1.CallTarget{ExtensionPoint: extpoint.ToolPackage, Capability: credentialCapability, Operation: &operation, LogicalService: &logicalService, RoutingDomain: &routingDomain}, call, payload)
		if err != nil || result.GetStatus() != contractv1.CallResult_STATUS_OK {
			return configurationv1.Observation{}, fmt.Errorf("retire old credential")
		}
	}
	candidate.retirePending = nil
	c.candidates[request.CandidateID] = candidate
	return c.observation(candidate), nil
}

func (c *hotWorkflowController) Abort(_ context.Context, _ sdk.Host, _ *contractv1.CallContext, request configurationv1.CandidateRequest) (configurationv1.Observation, error) {
	candidate, ok := c.candidates[request.CandidateID]
	if !ok || candidate.digest != request.RequestDigest || candidate.status == configurationv1.StatusCommitted {
		return configurationv1.Observation{}, fmt.Errorf("candidate cannot abort")
	}
	candidate.status = configurationv1.StatusAborted
	c.candidates[request.CandidateID] = candidate
	return c.observation(candidate), nil
}

func (c *hotWorkflowController) Status(_ context.Context, _ sdk.Host, _ *contractv1.CallContext, request configurationv1.StatusRequest) (configurationv1.Observation, error) {
	if request.ConfigurationID != c.configurationID {
		return configurationv1.Observation{}, fmt.Errorf("configuration not found")
	}
	if request.CandidateID == "" {
		return c.observation(hotWorkflowControllerCandidate{}), nil
	}
	candidate, ok := c.candidates[request.CandidateID]
	if !ok || candidate.digest != request.RequestDigest {
		return configurationv1.Observation{}, fmt.Errorf("candidate not found")
	}
	return c.observation(candidate), nil
}

func (c *hotWorkflowController) observation(candidate hotWorkflowControllerCandidate) configurationv1.Observation {
	observation := configurationv1.Observation{Protocol: configurationv1.Protocol, ConfigurationID: c.configurationID, Active: c.active, ObservedAt: time.Now().UTC()}
	if candidate.digest != "" {
		observation.Candidate = &configurationv1.CandidateObservation{
			CandidateID: candidate.request.CandidateID, RequestDigest: candidate.digest, ConfigurationDigest: candidate.config,
			Status: candidate.status, Ready: candidate.status != configurationv1.StatusAborted,
		}
	}
	return observation
}

func (h *hotWorkflowHost) Call(ctx context.Context, target *contractv1.CallTarget, call *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
	if target.GetExtensionPoint() == extpoint.KernelService && target.GetCapability() == pluginconfiguration.KernelCatalogsService && target.GetOperation() == "list" {
		raw, _ := json.Marshal(map[string]any{"items": []pluginconfiguration.Catalog{h.catalog}})
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
	}
	if target.GetExtensionPoint() == extpoint.KernelService && target.GetCapability() == configurationauthority.KernelIssueService && target.GetOperation() == "issue" {
		var request configurationauthority.IssueRequest
		_ = json.Unmarshal(payload, &request)
		if request.ConfigurationID != h.definition.ID || request.FieldID != "token" {
			return nil, nil, fmt.Errorf("unexpected authority request: %+v", request)
		}
		raw, _ := json.Marshal(configurationauthority.Issued{Token: configurationauthority.TokenPrefix + strings.Repeat("1", 64), ExpiresAt: time.Now().UTC().Add(time.Minute)})
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
	}
	if target.GetExtensionPoint() == extpoint.ToolPackage && target.GetCapability() == credentialCapability {
		switch target.GetOperation() {
		case "stageDelegated":
			h.stageCalls++
			raw, _ := json.Marshal(pluginconfig.StagedCredential{ID: "stage-" + strings.Repeat("2", 32), Ref: hotCredentialRef(h.definition.PluginID, "new", 2)})
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
		case "prepareDelegated":
			h.prepareCalls++
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, []byte(`{}`), nil
		case "activateDelegated":
			h.activateCalls++
			if h.failActivate {
				return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: "test.activation_failed", Message: "activation failed"}}, nil, nil
			}
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, []byte(`{}`), nil
		case "abortDelegated":
			h.abortCalls++
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, []byte(`{}`), nil
		case "retireManaged":
			var request map[string]string
			_ = json.Unmarshal(payload, &request)
			h.retired = append(h.retired, request["handle"])
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, []byte(`{}`), nil
		}
	}
	if target.GetExtensionPoint() != configurationv1.ExtensionPoint || target.GetCapability() != h.controllerID || target.GetLogicalService() != "authentication-otp" || target.GetRoutingDomain() != "security" {
		return nil, nil, fmt.Errorf("unexpected hot target: %+v", target)
	}
	handler := h.controller.Handlers[target.GetOperation()]
	if handler == nil {
		return nil, nil, fmt.Errorf("missing hot operation %s", target.GetOperation())
	}
	nested := &contractv1.CallContext{
		TenantId: call.GetTenantId(), Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: PluginID},
		Principal: call.GetPrincipal(),
	}
	return handler(ctx, h, nested, payload)
}

func TestHotServiceWorkflowRequiresSeparateApprovalAndRecovers(t *testing.T) {
	serviceFile := filepath.Join(t.TempDir(), "plugin-settings.json")
	service, err := newTestService(serviceFile)
	if err != nil {
		t.Fatal(err)
	}
	host, definition, controller := hotWorkflowFixture(t)
	alice, bob := userCall("tenant-a", "alice"), userCall("tenant-a", "bob")
	nextValues := hotTestValues("urn:test:new")
	draft, err := service.CreateDraft(context.Background(), host, alice, pluginconfiguration.CreateDraftRequest{
		ConfigurationID: definition.ID, CatalogDigest: host.catalog.Digest, Values: nextValues,
	})
	if err != nil || draft.ApplyPath != pluginconfiguration.ApplyHotService {
		t.Fatalf("无法创建 hot-service Draft: candidate=%+v err=%v", draft, err)
	}
	service, err = newTestService(serviceFile)
	if err != nil {
		t.Fatalf("hot-service Draft 基线无法跨重启恢复: %v", err)
	}
	pending, err := service.SubmitHotServiceDraft(context.Background(), host, alice, draft.ID, draft.Revision)
	if err != nil || pending.Status != pluginconfiguration.CandidatePublishing || pending.ExternalStatus != string(configurationv1.StatusPrepared) {
		t.Fatalf("hot-service 未进入独立审批: candidate=%+v err=%v", pending, err)
	}
	if _, err := service.ApproveHotServiceCandidate(alice, pending.ID, pending.Revision); err == nil {
		t.Fatal("hot-service 提交人不得自批")
	}
	approved, err := service.ApproveHotServiceCandidate(bob, pending.ID, pending.Revision)
	if err != nil || approved.ExternalStatus != string(hotApproved) {
		t.Fatalf("hot-service 审批失败: candidate=%+v err=%v", approved, err)
	}

	restarted, err := newTestService(serviceFile)
	if err != nil {
		t.Fatalf("plugin-settings 重启无法恢复 hot activation: %v", err)
	}
	ready, err := restarted.ActivateHotServiceCandidate(context.Background(), host, bob, approved.ID, approved.Revision)
	if err != nil || ready.Status != pluginconfiguration.CandidateReady || ready.ExternalStatus != string(configurationv1.StatusCommitted) || ready.ExternalRevision != 2 {
		t.Fatalf("hot-service 未原子激活: candidate=%+v err=%v", ready, err)
	}
	status, err := controller.Status(context.Background(), nil, nil, configurationv1.StatusRequest{ConfigurationID: definition.ID})
	if err != nil || status.Active.Revision != 2 {
		t.Fatalf("OTP controller Active revision 错误: status=%+v err=%v", status, err)
	}
	if _, err := newTestService(serviceFile); err != nil {
		t.Fatalf("Ready hot activation 持久状态无效: %v", err)
	}
	views := restarted.publicDefinitions("tenant-a", []pluginconfiguration.Catalog{host.catalog})
	if len(views) != 1 || !sameJSON(views[0].Values, nextValues) || views[0].Controller != nil || !views[0].ControllerAvailable || len(views[0].CredentialStates) != 1 || views[0].CredentialStates[0].Version != 1 {
		t.Fatalf("公开定义未投影最近一次 Ready hot values 或泄露了控制器目标: %+v", views)
	}
}

func TestHotServiceManagedCredentialCommitsBeforeActivationAndRecovers(t *testing.T) {
	serviceFile := filepath.Join(t.TempDir(), "plugin-settings.json")
	service, err := newTestService(serviceFile)
	if err != nil {
		t.Fatal(err)
	}
	host, definition, controller := hotWorkflowFixture(t)
	alice, bob := userCall("tenant-a", "alice"), userCall("tenant-a", "bob")
	draft, err := service.CreateDraft(context.Background(), host, alice, pluginconfiguration.CreateDraftRequest{
		ConfigurationID: definition.ID, CatalogDigest: host.catalog.Digest, Values: hotTestValues("urn:test:rotated"), Secrets: map[string]string{"token": "replacement-secret"},
	})
	if err != nil || host.stageCalls != 1 || draft.ManagedCredentials[0].Version != 2 {
		t.Fatalf("无法暂存 Service Hot 替换凭证: candidate=%+v calls=%d err=%v", draft, host.stageCalls, err)
	}
	pending, err := service.SubmitHotServiceDraft(context.Background(), host, alice, draft.ID, draft.Revision)
	if err != nil || host.prepareCalls != 1 {
		t.Fatalf("无法准备 Service Hot 替换凭证: candidate=%+v calls=%d err=%v", pending, host.prepareCalls, err)
	}
	approved, err := service.ApproveHotServiceCandidate(bob, pending.ID, pending.Revision)
	if err != nil {
		t.Fatal(err)
	}
	host.failActivate = true
	if _, err := service.ActivateHotServiceCandidate(context.Background(), host, bob, approved.ID, approved.Revision); err == nil {
		t.Fatal("凭证激活故障必须保留可恢复的 Activating 检查点")
	}
	status, _ := controller.Status(context.Background(), nil, nil, configurationv1.StatusRequest{ConfigurationID: definition.ID})
	if status.Active.Revision != 2 || controller.activeCredentials["token"].Version != 2 || host.activateCalls != 1 || !reflect.DeepEqual(host.retired, []string{"credential://managed/old"}) {
		t.Fatalf("控制器必须先提交完整引用集再激活新凭证: status=%+v active=%+v activate=%d retired=%v", status, controller.activeCredentials, host.activateCalls, host.retired)
	}
	interrupted, _ := service.ListCandidates(bob)
	if len(interrupted) != 1 || interrupted[0].Status != pluginconfiguration.CandidateActivating || interrupted[0].ManagedCredentials[0].State != "Candidate" {
		t.Fatalf("提交后激活失败未保留 Candidate 凭证检查点: %+v", interrupted)
	}
	host.failActivate = false
	restarted, err := newTestService(serviceFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.recoverInterrupted(context.Background(), host, bob); err != nil {
		t.Fatalf("无法从 Committed 控制器事实恢复凭证激活: %v", err)
	}
	items, _ := restarted.ListCandidates(bob)
	if len(items) != 1 || items[0].Status != pluginconfiguration.CandidateReady || items[0].ManagedCredentials[0].State != "Active" || items[0].ManagedCredentials[0].Staged || items[0].ManagedCredentials[0].Version != 2 || host.activateCalls != 2 {
		t.Fatalf("Service Hot 凭证恢复结果无效: items=%+v calls=%d", items, host.activateCalls)
	}
	views := restarted.publicDefinitions("tenant-a", []pluginconfiguration.Catalog{host.catalog})
	if len(views) != 1 || !views[0].ControllerAvailable || len(views[0].CredentialStates) != 1 || !views[0].CredentialStates[0].Configured || views[0].CredentialStates[0].Version != 2 {
		t.Fatalf("公开目录未投影新的安全凭证版本: %+v", views)
	}
	publicJSON, _ := json.Marshal(map[string]any{"definitions": views, "candidates": items})
	if strings.Contains(string(publicJSON), "credential://") || strings.Contains(string(publicJSON), "stage-") || strings.Contains(string(publicJSON), "replacement-secret") {
		t.Fatalf("公开 Service Hot 状态泄露了私有凭证事实: %s", publicJSON)
	}
	nextDraft, err := restarted.CreateDraft(context.Background(), host, alice, pluginconfiguration.CreateDraftRequest{
		ConfigurationID: definition.ID, CatalogDigest: host.catalog.Digest, Values: hotTestValues("urn:test:values-only"),
	})
	if err != nil || len(nextDraft.ManagedCredentials) != 1 || nextDraft.ManagedCredentials[0].State != "Retained" || nextDraft.ManagedCredentials[0].Version != 2 || host.stageCalls != 1 {
		t.Fatalf("后续 Draft 未从 Hot Active 安全投影保留凭证版本: candidate=%+v stageCalls=%d err=%v", nextDraft, host.stageCalls, err)
	}
}

func TestHotServicePendingCandidateCanAbortWithoutChangingActive(t *testing.T) {
	service, err := newTestService(filepath.Join(t.TempDir(), "plugin-settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	host, definition, controller := hotWorkflowFixture(t)
	alice := userCall("tenant-a", "alice")
	before, _ := controller.Status(context.Background(), nil, nil, configurationv1.StatusRequest{ConfigurationID: definition.ID})
	draft, err := service.CreateDraft(context.Background(), host, alice, pluginconfiguration.CreateDraftRequest{
		ConfigurationID: definition.ID, CatalogDigest: host.catalog.Digest, Values: hotTestValues("urn:test:discarded"),
	})
	if err != nil {
		t.Fatal(err)
	}
	pending, err := service.SubmitHotServiceDraft(context.Background(), host, alice, draft.ID, draft.Revision)
	if err != nil {
		t.Fatal(err)
	}
	aborted, err := service.AbortHotServiceCandidate(context.Background(), host, alice, pending.ID, pending.Revision)
	if err != nil || aborted.Status != pluginconfiguration.CandidateRolledBack || aborted.ExternalStatus != string(configurationv1.StatusAborted) || aborted.ManagedCredentials[0].State != "Retained" {
		t.Fatalf("hot-service abort 失败: candidate=%+v err=%v", aborted, err)
	}
	after, _ := controller.Status(context.Background(), nil, nil, configurationv1.StatusRequest{ConfigurationID: definition.ID})
	if before.Active != after.Active {
		t.Fatalf("abort 不得改变 Active: before=%+v after=%+v", before.Active, after.Active)
	}
}

func TestHotActivationStateRejectsPublicProjectionDrift(t *testing.T) {
	host, definition, _ := hotWorkflowFixture(t)
	controller := *definition.Controller
	request := configurationv1.PrepareRequest{
		CandidateID: "pcfg_" + strings.Repeat("f", 32), ConfigurationID: definition.ID,
		CatalogDigest: host.catalog.Digest, SchemaDigest: definition.SchemaDigest, ArtifactSHA256: definition.Artifact.SHA256,
		ExpectedActive: configurationv1.ActiveReference{Revision: 1, Digest: strings.Repeat("a", 64)}, Values: hotTestValues("urn:test:next"),
	}
	digest, err := configurationv1.DigestPrepareRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	record := hotActivationRecord{
		Target: controller, Prepare: request, RequestDigest: digest, Status: hotPendingApproval,
		SubmittedBy: "alice", CreatedAt: "2026-07-23T00:00:00Z", UpdatedAt: "2026-07-23T00:00:01Z",
	}
	candidate := pluginconfiguration.Candidate{
		ID: request.CandidateID, ConfigurationID: definition.ID, Revision: 2, Status: pluginconfiguration.CandidateReady,
		ApplyPath: pluginconfiguration.ApplyHotService, CatalogDigest: request.CatalogDigest, SchemaDigest: request.SchemaDigest,
		ArtifactSHA256: request.ArtifactSHA256, Values: request.Values, ExternalStatus: string(configurationv1.StatusPrepared),
	}
	if err := record.validate(candidate, "tenant-a"); err == nil {
		t.Fatal("持久 hot 状态与公开候选状态漂移时必须 fail-closed")
	}
}

func TestHotServiceDraftRejectsActiveChangedAfterCreation(t *testing.T) {
	service, err := newTestService(filepath.Join(t.TempDir(), "plugin-settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	host, definition, controller := hotWorkflowFixture(t)
	alice := userCall("tenant-a", "alice")
	draft, err := service.CreateDraft(context.Background(), host, alice, pluginconfiguration.CreateDraftRequest{
		ConfigurationID: definition.ID, CatalogDigest: host.catalog.Digest, Values: hotTestValues("urn:test:stale"),
	})
	if err != nil {
		t.Fatal(err)
	}
	controller.active = configurationv1.ActiveReference{Revision: controller.active.Revision + 1, Digest: strings.Repeat("9", 64)}
	if _, err := service.SubmitHotServiceDraft(context.Background(), host, alice, draft.ID, draft.Revision); !errors.Is(err, ErrConflict) {
		t.Fatalf("草稿创建后 Active 漂移必须拒绝提交: %v", err)
	}
}

func hotWorkflowFixture(t *testing.T) (*hotWorkflowHost, pluginconfiguration.Definition, *hotWorkflowController) {
	t.Helper()
	const pluginID = "cn.example.hot-controller"
	valuesRaw := hotTestValues("urn:test:old")
	var values map[string]any
	_ = json.Unmarshal(valuesRaw, &values)
	manifest := []byte(fmt.Sprintf(`{
		"id":%q,"name":"Hot Controller","description":"Hot config test","version":"1.0.0","publisher":"example","engines":{"backend":"^0.1"},
		"runtime":{"instancePolicy":"leader","stateModel":"leader-owned","visibility":"cluster","routing":"leader","routingDomain":"security"},
		"capabilities":{"kernelServices":["kernel.config.credential-ref"]},
		"configuration":{"scope":"service","applyMode":"hot","controller":{"protocol":"configuration.v1"},"schema":{"type":"object","additionalProperties":false,"required":["issuer"],"properties":{"issuer":{"type":"string"}}},"managedCredentials":[{"id":"token","title":"Service token","purpose":"remote.token","required":true}]},
		"activation":["onStartup"],"entry":{"backend":"backend/main"},"contributes":{"backend":{"tools":[{"id":"test.hot-controller","service_role":"backend","title":"Test","subcommands":[{"name":"status","description":"status"}]}]}}
	}`, pluginID))
	ref := pluginv1.ArtifactRef{PluginID: pluginID, Version: "1.0.0", Channel: "stable"}
	deployment := deploymentv2.Deployment{
		Version: 2, Revision: 1, Metadata: deploymentv1.Metadata{Name: "security-services", Tenant: "tenant-a"},
		Resolution: deploymentv2.Resolution{
			PlatformProfile: compositioncommonv1.Ref{ID: "platform-default", Revision: 1, Digest: strings.Repeat("1", 64)},
			PluginOrigins:   map[string]string{pluginID: deploymentv2.OriginPlatformProfile},
		},
		Units: []deploymentv2.ServiceUnit{{
			ID: "otp", Kind: "service", Enabled: true, ServiceRole: "backend", LogicalService: "authentication-otp", Replicas: 1,
			Plugins: []deploymentv1.PluginRef{{ID: pluginID, Version: "1.0.0", Channel: "stable"}},
			Config:  map[string]any{"plugins": map[string]any{pluginID: values}, "managed_credentials": map[string]any{pluginID: map[string]any{"token": hotCredentialRef(pluginID, "old", 1)}}},
		}},
	}
	catalog, err := pluginconfiguration.Build(deployment, map[pluginv1.ArtifactRef]pluginv1.Artifact{ref: {
		PluginID: ref.PluginID, Version: ref.Version, Channel: ref.Channel, SHA256: strings.Repeat("a", 64), Manifest: manifest,
	}})
	if err != nil {
		t.Fatal(err)
	}
	definition := catalog.Items[0]
	activeCredentials := map[string]pluginconfig.ManagedCredentialRef{"token": hotCredentialRef(pluginID, "old", 1)}
	activeDigest, _ := configurationv1.DigestConfiguration(definition.Values, activeCredentials)
	controller := &hotWorkflowController{configurationID: definition.ID, active: configurationv1.ActiveReference{Revision: 1, Digest: activeDigest}, activeCredentials: activeCredentials, candidates: map[string]hotWorkflowControllerCandidate{}}
	contribution, err := configurationcontroller.Contribution(pluginID, controller)
	if err != nil {
		t.Fatal(err)
	}
	return &hotWorkflowHost{catalog: catalog, controller: contribution, controllerID: contribution.ID, definition: definition}, definition, controller
}

func hotTestValues(issuer string) json.RawMessage {
	raw, _ := json.Marshal(map[string]any{"issuer": issuer})
	return raw
}

func hotCredentialRef(owner, suffix string, version int64) pluginconfig.ManagedCredentialRef {
	return pluginconfig.ManagedCredentialRef{Handle: "credential://managed/" + suffix, Scope: "tenant", Owner: owner, Purpose: "remote.token", Version: version}
}

func cloneCredentialRefs(source map[string]pluginconfig.ManagedCredentialRef) map[string]pluginconfig.ManagedCredentialRef {
	clone := make(map[string]pluginconfig.ManagedCredentialRef, len(source))
	for fieldID, ref := range source {
		clone[fieldID] = ref
	}
	return clone
}
