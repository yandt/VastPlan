package workfloworchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	apiv1 "cdsoft.com.cn/VastPlan/contracts/schemas/api/v1"
	workflowv1 "cdsoft.com.cn/VastPlan/contracts/schemas/workflow/v1"
)

func TestManagementAPIPublishesListsAndBindsToTrustedService(t *testing.T) {
	ctx := context.Background()
	repository := newMemoryRepository()
	service := New()
	feature := registerPortalFeature(t, ctx, service, repository)
	definition := workflowv1.Definition{ID: "platform.portal.review", Revision: 1, FeatureID: feature.ID, EntryNodeID: "done", Nodes: []workflowv1.Node{{ID: "done", Type: workflowv1.CoreNode(workflowv1.NodeEnd), Result: workflowv1.ResultSucceeded}}}

	raw, err := handleManagementAPI(ctx, nil, nil, repository, service, Actor{ID: "admin"}, "apiPublishDefinition", managementPayload(t, "workflow.definitions.publish", "POST", definition))
	if err != nil {
		t.Fatal(err)
	}
	var ref workflowv1.DefinitionRef
	if err := json.Unmarshal(raw, &ref); err != nil || ref.Digest == "" {
		t.Fatalf("ref=%+v err=%v", ref, err)
	}
	raw, err = handleManagementAPI(ctx, nil, nil, repository, service, Actor{ID: "reader"}, "apiListDefinitions", managementPayload(t, "workflow.definitions.list", "GET", nil))
	if err != nil {
		t.Fatal(err)
	}
	var definitions []PublishedDefinition
	if err := json.Unmarshal(raw, &definitions); err != nil || len(definitions) != 1 {
		t.Fatalf("definitions=%+v err=%v", definitions, err)
	}
	raw, err = handleManagementAPI(ctx, nil, nil, repository, service, Actor{ID: "admin"}, "apiBindDefinition", managementPayload(t, "workflow.bindings.put", "PUT", map[string]any{"featureId": feature.ID, "definition": ref, "expectedRevision": 0}))
	if err != nil {
		t.Fatal(err)
	}
	var binding workflowv1.Binding
	if err := json.Unmarshal(raw, &binding); err != nil || binding.ServiceID != "workflow-service" {
		t.Fatalf("binding=%+v err=%v", binding, err)
	}
	if _, err := service.BindDefinition(ctx, repository, Actor{ID: "admin"}, "other-service", feature.ID, ref, 0); err != nil {
		t.Fatal(err)
	}
	raw, err = handleManagementAPI(ctx, nil, nil, repository, service, Actor{ID: "reader"}, "apiListBindings", managementPayload(t, "workflow.bindings.list", "GET", nil))
	if err != nil {
		t.Fatal(err)
	}
	var bindings []workflowv1.Binding
	if err := json.Unmarshal(raw, &bindings); err != nil || len(bindings) != 1 || bindings[0].ServiceID != "workflow-service" {
		t.Fatalf("bindings=%+v err=%v", bindings, err)
	}
}

func TestManagementAPIIsolatesServiceInstancesTasksAndMutations(t *testing.T) {
	ctx := context.Background()
	repository := newMemoryRepository()
	service := New()
	feature := registerPortalFeature(t, ctx, service, repository)
	digest := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	for _, serviceID := range []string{"workflow-service", "other-service"} {
		if _, err := service.Start(ctx, repository, Actor{ID: "author"}, workflowv1.StartRequest{ID: serviceID + "-instance", ServiceID: serviceID, FeatureID: feature.ID, Resource: workflowv1.ResourceRef{Kind: feature.ResourceKind, ID: serviceID}, ResourceDigest: digest, IdempotencyKey: serviceID}); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := handleManagementAPI(ctx, nil, nil, repository, service, Actor{ID: "reader"}, "apiListInstances", managementPayload(t, "workflow.instances.list", "GET", nil))
	if err != nil {
		t.Fatal(err)
	}
	var instances []workflowv1.Instance
	if err := json.Unmarshal(raw, &instances); err != nil || len(instances) != 1 || instances[0].ServiceID != "workflow-service" {
		t.Fatalf("instances=%+v err=%v", instances, err)
	}
	task := workflowv1.Task{ID: "other-task", InstanceID: "other-service-instance", NodeID: "review", Title: "Review", AllowedOutcomes: []string{"approved"}, Status: workflowv1.WorkPending, Revision: 1}
	if _, err := repository.Create(ctx, storedRecord{ID: task.ID, Kind: kindTask, ServiceID: "other-service", FeatureID: feature.ID, Status: string(task.Status), Document: mustDocument(task)}, "other-task"); err != nil {
		t.Fatal(err)
	}
	raw, err = handleManagementAPI(ctx, nil, nil, repository, service, Actor{ID: "reader"}, "apiListTasks", managementPayload(t, "workflow.tasks.list", "GET", nil))
	if err != nil {
		t.Fatal(err)
	}
	var tasks []workflowv1.Task
	if err := json.Unmarshal(raw, &tasks); err != nil || len(tasks) != 0 {
		t.Fatalf("tasks=%+v err=%v", tasks, err)
	}
	_, err = handleManagementAPI(ctx, nil, nil, repository, service, Actor{ID: "admin"}, "apiCompleteTask", managementPayload(t, "workflow.tasks.complete", "POST", workflowv1.CompleteTaskRequest{TaskID: task.ID, ExpectedRevision: 1, Outcome: "approved"}))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-service task completion err=%v", err)
	}
	_, err = handleManagementAPI(ctx, nil, nil, repository, service, Actor{ID: "admin"}, "apiCancelInstance", managementPayload(t, "workflow.instances.cancel", "POST", workflowv1.CancelRequest{InstanceID: "other-service-instance", ExpectedRevision: 1, Reason: "not mine"}))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-service cancellation err=%v", err)
	}
}

func TestManagementAPIGovernsWithTrustedServiceIdentity(t *testing.T) {
	ctx := context.Background()
	repository := newMemoryRepository()
	service := New()
	registerPortalFeature(t, ctx, service, repository)
	host := &governedOperationHost{}
	call := &contractv1.CallContext{TenantId: "tenant", Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_USER, Id: "author"}}
	body := map[string]any{
		"featureId":      "platform.portal.publication",
		"preparePayload": map[string]any{"portalId": "operations"},
		"idempotencyKey": "portal-publication:operations:3",
	}
	raw, err := handleManagementAPI(ctx, host, call, repository, service, Actor{ID: "author"}, "apiGovern", managementPayload(t, "workflow.operations.govern", "POST", body))
	if err != nil {
		t.Fatal(err)
	}
	var result workflowv1.GovernedOperationResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	if result.Instance.ServiceID != "workflow-service" || result.Instance.Status != workflowv1.InstanceSucceeded {
		t.Fatalf("instance=%+v", result.Instance)
	}
}

func TestManagementAPIGovernRejectsBrowserSuppliedServiceIdentity(t *testing.T) {
	body := map[string]any{
		"serviceId":      "forged-service",
		"featureId":      "platform.portal.publication",
		"preparePayload": map[string]any{"portalId": "operations"},
		"idempotencyKey": "portal-publication:operations:3",
	}
	if _, err := handleManagementAPI(context.Background(), &governedOperationHost{}, &contractv1.CallContext{TenantId: "tenant", Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_USER, Id: "author"}}, newMemoryRepository(), New(), Actor{ID: "author"}, "apiGovern", managementPayload(t, "workflow.operations.govern", "POST", body)); err == nil {
		t.Fatal("browser-supplied serviceId must be rejected")
	}
}

func TestManagementAPIRejectsUntrustedOrStaleTarget(t *testing.T) {
	invocation := apiv1.GatewayInvocation{SchemaVersion: apiv1.SchemaVersion, RouteID: "workflow.catalog.read", Method: "GET", PathParams: map[string]string{}, Query: map[string][]string{}, Body: json.RawMessage(`{}`), ManagementTarget: &apiv1.ManagementInvocationTarget{PortalID: "operations", ServiceID: "workflow-service", ActivationID: 7, Generation: 8}}
	raw, _ := json.Marshal(invocation)
	if _, err := managementInvocation(raw, "apiCatalog"); err == nil {
		t.Fatal("stale management target must be rejected")
	}
}

func managementPayload(t *testing.T, routeID, method string, body any) []byte {
	t.Helper()
	rawBody := json.RawMessage(`{}`)
	if body != nil {
		var err error
		rawBody, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	invocation := apiv1.GatewayInvocation{SchemaVersion: apiv1.SchemaVersion, RouteID: routeID, Method: method, PathParams: map[string]string{}, Query: map[string][]string{}, Body: rawBody, ManagementTarget: &apiv1.ManagementInvocationTarget{PortalID: "operations", ServiceID: "workflow-service", ActivationID: 7, Generation: 7}}
	raw, err := json.Marshal(invocation)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
