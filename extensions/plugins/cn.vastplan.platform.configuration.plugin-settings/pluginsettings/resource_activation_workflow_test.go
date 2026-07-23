package pluginsettings

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	configurationresourcev1 "cdsoft.com.cn/VastPlan/contracts/schemas/configurationresource/v1"
	deploymentv1 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v1"
	deploymentv2 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v2"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/configurationauthority"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfig"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfiguration"
)

type resourceWorkflowHost struct {
	catalog                                 pluginconfiguration.Catalog
	controller                              *resourceWorkflowController
	stageCalls, prepareCalls, activateCalls int
}

type resourceWorkflowController struct {
	collectionID string
	items        map[string]resourceWorkflowItem
	candidates   map[string]resourceWorkflowCandidate
}

type resourceWorkflowItem struct {
	revision    uint64
	digest      string
	values      json.RawMessage
	credentials map[string]pluginconfig.ManagedCredentialRef
}

type resourceWorkflowCandidate struct {
	request       configurationresourcev1.PrepareRequest
	requestDigest string
	resultDigest  string
	status        configurationresourcev1.CandidateStatus
}

func TestResourceProfileSecretSagaUsesExactIdentityAndRecovers(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "plugin-settings.json")
	service, err := New(stateFile)
	if err != nil {
		t.Fatal(err)
	}
	host, definition, collection := resourceWorkflowFixture(t)
	alice, bob := userCall("tenant-a", "alice"), userCall("tenant-a", "bob")
	draft, err := service.CreateResourceDraft(context.Background(), host, alice, resourceDraftRequest{
		ConfigurationID: definition.ID, ResourceCollectionID: collection.ID, CatalogDigest: host.catalog.Digest,
		Action:  configurationresourcev1.ActionCreate,
		Values:  json.RawMessage(`{"displayName":"Enterprise Mail","endpoint":"https://delivery.example.test/v1/code","channels":["email"],"timeoutMs":1000}`),
		Secrets: map[string]string{"authorization": "Bearer secret"},
	})
	if err != nil || draft.ApplyPath != pluginconfiguration.ApplyResourceProfile || !strings.HasPrefix(draft.ResourceID, "cfgp_") || host.stageCalls != 1 {
		t.Fatalf("无法创建带秘密的 Profile 资源草稿: candidate=%+v stage=%d err=%v", draft, host.stageCalls, err)
	}
	pending, err := service.SubmitResourceDraft(context.Background(), host, alice, draft.ID, draft.Revision)
	if err != nil || pending.ExternalStatus != string(configurationresourcev1.StatusPrepared) || host.prepareCalls != 1 {
		t.Fatalf("Profile 资源未进入审批: candidate=%+v prepare=%d err=%v", pending, host.prepareCalls, err)
	}
	if _, err := service.ApproveResourceCandidate(alice, pending.ID, pending.Revision); err == nil {
		t.Fatal("Profile 资源提交人不得自批")
	}
	approved, err := service.ApproveResourceCandidate(bob, pending.ID, pending.Revision)
	if err != nil {
		t.Fatal(err)
	}
	restarted, err := New(stateFile)
	if err != nil {
		t.Fatalf("资源 Saga 无法跨重启恢复: %v", err)
	}
	ready, err := restarted.ActivateResourceCandidate(context.Background(), host, bob, approved.ID, approved.Revision)
	if err != nil || ready.Status != pluginconfiguration.CandidateReady || ready.ExternalStatus != string(configurationresourcev1.StatusCommitted) || host.activateCalls != 1 {
		t.Fatalf("Profile 资源未原子激活: candidate=%+v activate=%d err=%v", ready, host.activateCalls, err)
	}
	response, err := restarted.GetResourceItem(context.Background(), host, bob, definition.ID, collection.ID, ready.ResourceID, host.catalog.Digest)
	if err != nil || response.Item.Active.Revision != 1 || len(response.Item.CredentialStates) != 1 || !response.Item.CredentialStates[0].Configured {
		t.Fatalf("激活资源查询错误: response=%+v err=%v", response, err)
	}
	raw, _ := json.Marshal(response)
	if strings.Contains(string(raw), "credential://") || strings.Contains(string(raw), "Bearer secret") {
		t.Fatal("资源查询泄露了凭证 handle 或 material")
	}
	if _, err := New(stateFile); err != nil {
		t.Fatalf("Ready resource activation 持久状态无效: %v", err)
	}
}

func (h *resourceWorkflowHost) Call(ctx context.Context, target *contractv1.CallTarget, call *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
	if target.GetCapability() == pluginconfiguration.KernelCatalogsService {
		raw, _ := json.Marshal(map[string]any{"items": []pluginconfiguration.Catalog{h.catalog}})
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
	}
	definition, collection := h.catalog.Items[0], h.catalog.Items[0].ResourceCollections[0]
	if target.GetCapability() == configurationauthority.KernelIssueService {
		var request configurationauthority.IssueRequest
		_ = json.Unmarshal(payload, &request)
		if request.ConfigurationID != definition.ID || request.ResourceCollectionID != collection.ID || !strings.HasPrefix(request.ResourceID, "cfgp_") || request.FieldID != "authorization" {
			return nil, nil, fmt.Errorf("unexpected resource authority: %+v", request)
		}
		raw, _ := json.Marshal(configurationauthority.Issued{Token: configurationauthority.TokenPrefix + strings.Repeat("1", 64), ExpiresAt: time.Now().UTC().Add(time.Minute)})
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
	}
	if target.GetCapability() == credentialCapability {
		switch target.GetOperation() {
		case "stageDelegated":
			h.stageCalls++
			raw, _ := json.Marshal(pluginconfig.StagedCredential{ID: "stage-" + strings.Repeat("2", 32), Ref: pluginconfig.ManagedCredentialRef{
				Handle: "credential://managed/" + strings.Repeat("3", 32), Scope: "tenant", Owner: definition.PluginID, Purpose: "authentication.delivery.webhook", Version: 1,
			}})
			return okResourceResult(raw)
		case "prepareDelegated":
			h.prepareCalls++
			return okResourceResult([]byte(`{}`))
		case "activateDelegated":
			h.activateCalls++
			return okResourceResult([]byte(`{}`))
		case "abortDelegated":
			return okResourceResult([]byte(`{}`))
		}
	}
	if target.GetExtensionPoint() == configurationresourcev1.ExtensionPoint && target.GetCapability() == definition.ResourceController.Capability && target.GetLogicalService() == "authentication-delivery" {
		raw, err := h.controller.call(target.GetOperation(), payload)
		if err != nil {
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: "test.rejected", Message: err.Error()}}, nil, nil
		}
		return okResourceResult(raw)
	}
	return nil, nil, fmt.Errorf("unexpected resource target: %+v", target)
}

func (c *resourceWorkflowController) call(operation string, raw []byte) ([]byte, error) {
	parsed, err := configurationresourcev1.ParseRequest(operation, raw)
	if err != nil {
		return nil, err
	}
	switch request := parsed.(type) {
	case *configurationresourcev1.ListRequest:
		items := make([]configurationresourcev1.ResourceView, 0, len(c.items))
		for id := range c.items {
			items = append(items, c.view(id))
		}
		return json.Marshal(configurationresourcev1.ListResponse{Protocol: configurationresourcev1.Protocol, CollectionID: c.collectionID, Items: items, ObservedAt: time.Now().UTC()})
	case *configurationresourcev1.GetRequest:
		if _, ok := c.items[request.ResourceID]; !ok {
			return nil, fmt.Errorf("resource not found")
		}
		return json.Marshal(configurationresourcev1.GetResponse{Protocol: configurationresourcev1.Protocol, CollectionID: c.collectionID, Item: c.view(request.ResourceID), ObservedAt: time.Now().UTC()})
	case *configurationresourcev1.PrepareRequest:
		if _, exists := c.items[request.ResourceID]; exists || request.Action != configurationresourcev1.ActionCreate {
			return nil, fmt.Errorf("create CAS conflict")
		}
		digest, _ := configurationresourcev1.DigestPrepareRequest(*request)
		result := digestResource(request.Values, request.ManagedCredentials)
		candidate := resourceWorkflowCandidate{request: *request, requestDigest: digest, resultDigest: result, status: configurationresourcev1.StatusPrepared}
		c.candidates[request.CandidateID] = candidate
		return json.Marshal(c.observation(candidate))
	case *configurationresourcev1.CandidateRequest:
		candidate, ok := c.candidates[request.CandidateID]
		if !ok || candidate.requestDigest != request.RequestDigest {
			return nil, fmt.Errorf("candidate not found")
		}
		if operation == configurationresourcev1.OperationCommit && candidate.status == configurationresourcev1.StatusPrepared {
			c.items[candidate.request.ResourceID] = resourceWorkflowItem{revision: 1, digest: candidate.resultDigest, values: candidate.request.Values, credentials: candidate.request.ManagedCredentials}
			candidate.status = configurationresourcev1.StatusCommitted
			c.candidates[request.CandidateID] = candidate
		}
		if operation == configurationresourcev1.OperationAbort && candidate.status == configurationresourcev1.StatusPrepared {
			candidate.status = configurationresourcev1.StatusAborted
			c.candidates[request.CandidateID] = candidate
		}
		return json.Marshal(c.observation(candidate))
	case *configurationresourcev1.StatusRequest:
		candidate, ok := c.candidates[request.CandidateID]
		if !ok || candidate.requestDigest != request.RequestDigest {
			return nil, fmt.Errorf("candidate not found")
		}
		return json.Marshal(c.observation(candidate))
	}
	return nil, fmt.Errorf("unsupported operation")
}

func (c *resourceWorkflowController) view(id string) configurationresourcev1.ResourceView {
	item := c.items[id]
	states := make([]configurationresourcev1.CredentialState, 0, len(item.credentials))
	for field, ref := range item.credentials {
		states = append(states, configurationresourcev1.CredentialState{FieldID: field, Configured: true, Version: ref.Version})
	}
	return configurationresourcev1.ResourceView{ResourceID: id, Active: configurationresourcev1.ActiveReference{Revision: item.revision, Digest: item.digest}, Values: item.values, CredentialStates: states, UpdatedAt: time.Now().UTC()}
}

func (c *resourceWorkflowController) observation(candidate resourceWorkflowCandidate) configurationresourcev1.Observation {
	observation := configurationresourcev1.Observation{Protocol: configurationresourcev1.Protocol, CollectionID: c.collectionID, ResourceID: candidate.request.ResourceID, ObservedAt: time.Now().UTC()}
	if item, ok := c.items[candidate.request.ResourceID]; ok {
		observation.Active = &configurationresourcev1.ActiveReference{Revision: item.revision, Digest: item.digest}
	}
	observation.Candidate = &configurationresourcev1.CandidateObservation{
		CandidateID: candidate.request.CandidateID, RequestDigest: candidate.requestDigest, ResultDigest: candidate.resultDigest,
		Action: candidate.request.Action, Status: candidate.status, Ready: candidate.status != configurationresourcev1.StatusAborted,
	}
	return observation
}

func resourceWorkflowFixture(t *testing.T) (*resourceWorkflowHost, pluginconfiguration.Definition, pluginconfiguration.ResourceCollection) {
	t.Helper()
	const pluginID = "cn.vastplan.demo-delivery-profile"
	manifest := []byte(fmt.Sprintf(`{
		"id":%q,"name":"Delivery","description":"delivery profiles","version":"1.0.0","publisher":"vastplan","engines":{"backend":"^0.1"},
		"runtime":{"instancePolicy":"leader","stateModel":"leader-owned","visibility":"cluster","routing":"leader","routingDomain":"security"},
		"configuration":{"scope":"service","applyMode":"restart","schema":{"type":"object","additionalProperties":false},"resourceController":{"protocol":"configuration.resource.v1"},"resourceCollections":[{"id":"delivery-profile","kind":"profile","title":"Delivery Profile","schema":{"type":"object","additionalProperties":false,"required":["displayName","endpoint","channels","timeoutMs"],"properties":{"displayName":{"type":"string"},"endpoint":{"type":"string"},"channels":{"type":"array","items":{"type":"string"}},"timeoutMs":{"type":"integer"}}},"managedCredentials":[{"id":"authorization","title":"Authorization","purpose":"authentication.delivery.webhook","required":true}],"maxItems":64}]},
		"activation":["onStartup"],"entry":{"backend":"backend/main"},"contributes":{"backend":{"tools":[]}}
	}`, pluginID))
	ref := pluginv1.ArtifactRef{PluginID: pluginID, Version: "1.0.0", Channel: "stable"}
	deployment := deploymentv2.Deployment{Version: 2, Revision: 1, Metadata: deploymentv1.Metadata{Name: "security", Tenant: "tenant-a"}, Resolution: deploymentv2.Resolution{PluginOrigins: map[string]string{pluginID: deploymentv2.OriginPlatformProfile}}, Units: []deploymentv2.ServiceUnit{{
		ID: "delivery", Kind: "service", Enabled: true, ServiceRole: "backend", LogicalService: "authentication-delivery", Replicas: 1,
		Plugins: []deploymentv1.PluginRef{{ID: pluginID, Version: "1.0.0", Channel: "stable"}}, Config: map[string]any{"plugins": map[string]any{pluginID: map[string]any{}}},
	}}}
	catalog, err := pluginconfiguration.Build(deployment, map[pluginv1.ArtifactRef]pluginv1.Artifact{ref: {PluginID: pluginID, Version: "1.0.0", Channel: "stable", SHA256: strings.Repeat("d", 64), Manifest: manifest}})
	if err != nil {
		t.Fatal(err)
	}
	definition, collection := catalog.Items[0], catalog.Items[0].ResourceCollections[0]
	controller := &resourceWorkflowController{collectionID: collection.ID, items: map[string]resourceWorkflowItem{}, candidates: map[string]resourceWorkflowCandidate{}}
	return &resourceWorkflowHost{catalog: catalog, controller: controller}, definition, collection
}

func digestResource(values json.RawMessage, credentials map[string]pluginconfig.ManagedCredentialRef) string {
	raw, _ := json.Marshal(struct {
		Values      json.RawMessage                              `json:"values"`
		Credentials map[string]pluginconfig.ManagedCredentialRef `json:"credentials,omitempty"`
	}{values, credentials})
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

func okResourceResult(raw []byte) (*contractv1.CallResult, []byte, error) {
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
}
