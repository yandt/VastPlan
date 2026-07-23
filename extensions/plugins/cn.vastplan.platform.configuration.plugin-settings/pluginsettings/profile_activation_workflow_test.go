package pluginsettings

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	deploymentv1 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v1"
	deploymentv2 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v2"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/platformprofileactivation"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfiguration"
)

type profileDraftHost struct {
	base         *credentialDraftHost
	status       platformprofileactivation.ActivationStatus
	profileCalls []string
	requestedBy  string
	approvedBy   string
}

func (h *profileDraftHost) Call(ctx context.Context, target *contractv1.CallTarget, call *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
	if target.GetCapability() != platformprofileactivation.DeploymentCapability || !isProfileOperation(target.GetOperation()) {
		return h.base.Call(ctx, target, call, payload)
	}
	h.profileCalls = append(h.profileCalls, target.GetOperation())
	var request struct {
		CandidateID       string                                            `json:"candidateId"`
		ProfileActivation platformprofileactivation.CreateActivationRequest `json:"profileActivation"`
	}
	_ = json.Unmarshal(payload, &request)
	if request.ProfileActivation.CandidateID != "" {
		request.CandidateID = request.ProfileActivation.CandidateID
	}
	if h.status == "" {
		h.status, h.requestedBy = platformprofileactivation.ActivationPendingApproval, call.GetPrincipal().GetUserId()
	}
	switch target.GetOperation() {
	case platformprofileactivation.ApproveActivationOperation:
		if call.GetPrincipal().GetUserId() == h.requestedBy {
			return nil, nil, fmt.Errorf("separation required")
		}
		h.status, h.approvedBy = platformprofileactivation.ActivationApproved, call.GetPrincipal().GetUserId()
	case platformprofileactivation.PublishActivationOperation:
		if h.status != platformprofileactivation.ActivationApproved {
			return nil, nil, fmt.Errorf("not approved")
		}
		h.status = platformprofileactivation.ActivationReady
	case platformprofileactivation.AbortActivationOperation:
		h.status = platformprofileactivation.ActivationAborted
	}
	activation := platformprofileactivation.Activation{
		CandidateID: request.CandidateID, ConfigurationID: h.base.definition.ID, Deployment: h.base.definition.Deployment,
		DeploymentRevision: 9, PreviousServiceRevision: h.base.definition.DeploymentRevision,
		Status: h.status, RequestedBy: h.requestedBy, ApprovedBy: h.approvedBy,
	}
	raw, _ := json.Marshal(activation)
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
}

func isProfileOperation(operation string) bool {
	switch operation {
	case platformprofileactivation.CreateActivationOperation, platformprofileactivation.GetActivationOperation,
		platformprofileactivation.ApproveActivationOperation, platformprofileactivation.PublishActivationOperation,
		platformprofileactivation.AbortActivationOperation:
		return true
	default:
		return false
	}
}

func TestPlatformProfileDraftUsesDedicatedApprovalAndCredentialSaga(t *testing.T) {
	catalog := managedPlatformTestCatalog(t)
	base := &credentialDraftHost{catalogHost: catalogHost{catalogs: []pluginconfiguration.Catalog{catalog}}, definition: catalog.Items[0]}
	host := &profileDraftHost{base: base}
	stateFile := filepath.Join(t.TempDir(), "plugin-settings.json")
	service, err := newTestService(stateFile)
	if err != nil {
		t.Fatal(err)
	}
	alice, bob := userCall("tenant-a", "alice"), userCall("tenant-a", "bob")
	draft, err := service.CreateDraft(context.Background(), host, alice, pluginconfiguration.CreateDraftRequest{
		ConfigurationID: catalog.Items[0].ID, CatalogDigest: catalog.Digest,
		Values: []byte(`{"region":"cn-west"}`), Secrets: map[string]string{"token": "secret"},
	})
	if err != nil || draft.ApplyPath != pluginconfiguration.ApplyPlatformProfile {
		t.Fatalf("未创建 Platform Profile 草稿: candidate=%+v err=%v", draft, err)
	}
	if _, err := service.SubmitDraft(context.Background(), host, alice, draft.ID, draft.Revision); err == nil {
		t.Fatal("Platform Profile 草稿不得走 Application Deployment 发布权限")
	}
	submitted, err := service.SubmitProfileDraft(context.Background(), host, alice, draft.ID, draft.Revision)
	if err != nil || submitted.ExternalStatus != string(platformprofileactivation.ActivationPendingApproval) || submitted.ExternalRevision != 9 {
		t.Fatalf("Profile 草稿未进入专用审批: candidate=%+v err=%v", submitted, err)
	}
	reopened, err := newTestService(stateFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.recoverInterrupted(context.Background(), host, alice); err != nil {
		t.Fatalf("重启后未恢复 Profile 待审批状态: %v", err)
	}
	items, _ := reopened.ListCandidates(alice)
	submitted = items[0]
	approved, err := reopened.ApproveProfileCandidate(context.Background(), host, bob, submitted.ID, submitted.Revision)
	if err != nil || approved.ExternalStatus != string(platformprofileactivation.ActivationApproved) {
		t.Fatalf("Profile 候选未完成异人审批: candidate=%+v err=%v", approved, err)
	}
	ready, err := reopened.ActivateProfileCandidate(context.Background(), host, bob, approved.ID, approved.Revision)
	if err != nil || ready.Status != pluginconfiguration.CandidateReady || ready.ExternalStatus != string(platformprofileactivation.ActivationReady) || base.prepareCalls != 1 || base.activateCalls != 1 {
		t.Fatalf("Profile 候选未完成凭证和部署 Saga: candidate=%+v prepare=%d activate=%d err=%v", ready, base.prepareCalls, base.activateCalls, err)
	}
	if got := strings.Join(host.profileCalls, ","); !strings.Contains(got, platformprofileactivation.CreateActivationOperation) || !strings.Contains(got, platformprofileactivation.ApproveActivationOperation) || !strings.Contains(got, platformprofileactivation.PublishActivationOperation) {
		t.Fatalf("未经过专用 Profile Activation 操作: %s", got)
	}
}

func managedPlatformTestCatalog(t *testing.T) pluginconfiguration.Catalog {
	t.Helper()
	const pluginID = "com.example.managed-platform"
	manifest := []byte(fmt.Sprintf(`{"id":%q,"name":"Managed platform","description":"managed","version":"1.0.0","publisher":"example","engines":{"backend":"^1.0"},"capabilities":{"kernelServices":["kernel.config.credential-ref"]},"configuration":{"scope":"service","applyMode":"restart","schema":{"type":"object","additionalProperties":false,"required":["region"],"properties":{"region":{"type":"string"}}},"managedCredentials":[{"id":"token","title":"Token","purpose":"remote.token","required":true}]},"activation":["onStartup"],"entry":{"backend":"backend/main"},"contributes":{"backend":{"tools":[]}}}`, pluginID))
	ref := pluginv1.ArtifactRef{PluginID: pluginID, Version: "1.0.0", Channel: "stable"}
	deployment := deploymentv2.Deployment{
		Version: 2, Revision: 8, Metadata: deploymentv1.Metadata{Name: "managed-services", Tenant: "tenant-a"},
		Resolution: deploymentv2.Resolution{PluginOrigins: map[string]string{pluginID: deploymentv2.OriginPlatformProfile}},
		Units: []deploymentv2.ServiceUnit{{
			ID: "platform-api", Kind: "service", Enabled: true, ServiceRole: "backend", Replicas: 1,
			Plugins: []deploymentv1.PluginRef{{ID: pluginID, Version: "1.0.0", Channel: "stable"}},
			Config:  map[string]any{"plugins": map[string]any{pluginID: map[string]any{"region": "cn-east"}}},
		}},
	}
	catalog, err := pluginconfiguration.Build(deployment, map[pluginv1.ArtifactRef]pluginv1.Artifact{ref: {PluginID: pluginID, Version: "1.0.0", Channel: "stable", SHA256: strings.Repeat("b", 64), Manifest: manifest}})
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}
