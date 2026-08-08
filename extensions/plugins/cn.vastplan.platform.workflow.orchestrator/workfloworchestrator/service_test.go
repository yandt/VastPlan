package workfloworchestrator

import (
	"context"
	"strings"
	"testing"
	"time"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	workflowv1 "cdsoft.com.cn/VastPlan/contracts/schemas/workflow/v1"
)

func TestWorkflowRunsManualTaskActionAndTerminalState(t *testing.T) {
	ctx := context.Background()
	repository := newMemoryRepository()
	service := New()
	now := time.Date(2026, 8, 9, 1, 2, 3, 0, time.UTC)
	service.now = func() time.Time { return now }
	feature := registerPortalFeature(t, ctx, service, repository)
	definition := workflowv1.Definition{ID: "platform.portal.release", Revision: 1, FeatureID: feature.ID, EntryNodeID: "review", Nodes: []workflowv1.Node{
		{ID: "review", Type: workflowv1.CoreNode(workflowv1.NodeManual), Title: "Review publication", Roles: []string{"portal.approver"}, Outcomes: map[string]string{"approved": "release", "rejected": "rejected"}},
		{ID: "release", Type: workflowv1.CoreNode(workflowv1.NodeAction), ActionID: "portal.release", Next: "done"},
		{ID: "done", Type: workflowv1.CoreNode(workflowv1.NodeEnd), Result: workflowv1.ResultSucceeded},
		{ID: "rejected", Type: workflowv1.CoreNode(workflowv1.NodeEnd), Result: workflowv1.ResultRejected},
	}}
	definitionRef, err := service.PublishDefinition(ctx, repository, Actor{ID: "admin"}, definition)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.BindDefinition(ctx, repository, Actor{ID: "admin"}, "portal-service", feature.ID, definitionRef, 0); err != nil {
		t.Fatal(err)
	}
	request := workflowv1.StartRequest{ID: "publication-1", ServiceID: "portal-service", FeatureID: feature.ID, Resource: workflowv1.ResourceRef{Kind: feature.ResourceKind, ID: "publication-1"}, ResourceDigest: strings.Repeat("a", 64), IdempotencyKey: "submit-publication-1"}
	instance, err := service.Start(ctx, repository, Actor{ID: "author"}, request)
	if err != nil || instance.CurrentNodeID != "review" {
		t.Fatalf("instance=%+v err=%v", instance, err)
	}
	duplicate, err := service.Start(ctx, repository, Actor{ID: "author"}, request)
	if err != nil || duplicate.ID != instance.ID {
		t.Fatalf("idempotent start=%+v err=%v", duplicate, err)
	}
	tasks, err := service.ListTasks(ctx, repository, Actor{ID: "reviewer", Roles: []string{"portal.approver"}})
	if err != nil || len(tasks) != 1 {
		t.Fatalf("tasks=%+v err=%v", tasks, err)
	}
	if hidden, _ := service.ListTasks(ctx, repository, Actor{ID: "reader", Roles: []string{"portal.reader"}}); len(hidden) != 0 {
		t.Fatalf("task leaked: %+v", hidden)
	}
	instance, err = service.CompleteTask(ctx, repository, Actor{ID: "reviewer", Roles: []string{"portal.approver"}}, workflowv1.CompleteTaskRequest{TaskID: tasks[0].ID, ExpectedRevision: tasks[0].Revision, Outcome: "approved"})
	if err != nil || instance.CurrentNodeID != "release" {
		t.Fatalf("after task=%+v err=%v", instance, err)
	}
	work, action, actionRequest, err := service.PendingAction(ctx, repository, instance.ID)
	if err != nil || action.Operation != "executePublicationRelease" || actionRequest.ResourceDigest != request.ResourceDigest {
		t.Fatalf("work=%+v action=%+v request=%+v err=%v", work, action, actionRequest, err)
	}
	instance, err = service.CompleteAction(ctx, repository, workflowv1.CompleteActionRequest{WorkID: work.ID, ExpectedRevision: work.Revision, Succeeded: true})
	if err != nil || instance.Status != workflowv1.InstanceSucceeded || instance.CurrentNodeID != "" {
		t.Fatalf("completed=%+v err=%v", instance, err)
	}
}

func TestWorkflowRejectsRevisionGapsAndTaskCASRaces(t *testing.T) {
	ctx := context.Background()
	repository := newMemoryRepository()
	service := New()
	feature := registerPortalFeature(t, ctx, service, repository)
	definition := workflowv1.Definition{ID: "platform.portal.review", Revision: 2, FeatureID: feature.ID, EntryNodeID: "done", Nodes: []workflowv1.Node{{ID: "done", Type: workflowv1.CoreNode(workflowv1.NodeEnd), Result: workflowv1.ResultSucceeded}}}
	if _, err := service.PublishDefinition(ctx, repository, Actor{ID: "admin"}, definition); err == nil {
		t.Fatal("revision gap must be rejected")
	}
	definition.Revision = 1
	definition.EntryNodeID = "review"
	definition.Nodes = []workflowv1.Node{{ID: "review", Type: workflowv1.CoreNode(workflowv1.NodeManual), Title: "Review", Outcomes: map[string]string{"approved": "done"}}, {ID: "done", Type: workflowv1.CoreNode(workflowv1.NodeEnd), Result: workflowv1.ResultSucceeded}}
	ref, err := service.PublishDefinition(ctx, repository, Actor{ID: "admin"}, definition)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.BindDefinition(ctx, repository, Actor{ID: "admin"}, "portal-service", feature.ID, ref, 0); err != nil {
		t.Fatal(err)
	}
	instance, err := service.Start(ctx, repository, Actor{ID: "author"}, workflowv1.StartRequest{ID: "cancel-me", ServiceID: "portal-service", FeatureID: feature.ID, Resource: workflowv1.ResourceRef{Kind: feature.ResourceKind, ID: "candidate"}, ResourceDigest: strings.Repeat("a", 64), IdempotencyKey: "cancel-start"})
	if err != nil {
		t.Fatal(err)
	}
	cancelled, err := service.Cancel(ctx, repository, Actor{ID: "admin"}, workflowv1.CancelRequest{InstanceID: instance.ID, ExpectedRevision: instance.Revision, Reason: "candidate withdrawn"})
	if err != nil || cancelled.Status != workflowv1.InstanceCancelled || cancelled.CurrentNodeID != "" {
		t.Fatalf("cancelled=%+v err=%v", cancelled, err)
	}
	if _, err := service.Cancel(ctx, repository, Actor{ID: "admin"}, workflowv1.CancelRequest{InstanceID: instance.ID, ExpectedRevision: cancelled.Revision, Reason: "again"}); err == nil {
		t.Fatal("terminal instance must not be cancelled twice")
	}
}

func TestWorkflowUsesAuditedDirectActionWithoutBinding(t *testing.T) {
	ctx := context.Background()
	repository := newMemoryRepository()
	service := New()
	feature := registerPortalFeature(t, ctx, service, repository)
	request := workflowv1.StartRequest{ID: "direct-publication", ServiceID: "portal-service", FeatureID: feature.ID, Resource: workflowv1.ResourceRef{Kind: feature.ResourceKind, ID: "candidate"}, ResourceDigest: strings.Repeat("a", 64), IdempotencyKey: "direct-start"}

	instance, err := service.Start(ctx, repository, Actor{ID: "author"}, request)
	if err != nil || instance.Mode != workflowv1.ExecutionDirect || instance.CurrentNodeID != "direct" {
		t.Fatalf("instance=%+v err=%v", instance, err)
	}
	duplicate, err := service.Start(ctx, repository, Actor{ID: "author"}, request)
	if err != nil || duplicate.ID != instance.ID || duplicate.Mode != workflowv1.ExecutionDirect {
		t.Fatalf("duplicate=%+v err=%v", duplicate, err)
	}
	work, action, _, err := service.PendingAction(ctx, repository, instance.ID)
	if err != nil || action.ID != feature.UnboundActionID {
		t.Fatalf("work=%+v action=%+v err=%v", work, action, err)
	}
	completed, err := service.CompleteAction(ctx, repository, workflowv1.CompleteActionRequest{WorkID: work.ID, ExpectedRevision: work.Revision, Succeeded: true})
	if err != nil || completed.Status != workflowv1.InstanceSucceeded || completed.Mode != workflowv1.ExecutionDirect {
		t.Fatalf("completed=%+v err=%v", completed, err)
	}
}

func TestRegisterCatalogPinsSignedNodeOwners(t *testing.T) {
	ctx := context.Background()
	repository := newMemoryRepository()
	service := New()
	raw := []byte(`{"id":"cn.vastplan.integration.authentication.confirmation","name":"confirmation","description":"confirmation","version":"2.1.0","publisher":"vastplan","engines":{"backend":"^0.1"},"activation":["onStartup"],"entry":{"backend":"backend/main"},"contributes":{"backend":{"workflowNodeTemplates":[{"id":"authentication.email-confirmation","contract":"1.0.0","title":"Email confirmation","compilerContract":"workflow.node-template.v1","configurationSchema":{"path":"workflow-nodes/email/config.schema.json","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"expansion":{"path":"workflow-nodes/email/expansion.json","sha256":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},"outcomes":["confirmed","expired"]}],"workflowNodeProviders":[{"id":"authentication.phone-confirmation","contract":"1.0.0","title":"Phone confirmation","effectContract":"workflow.node-effect.v1","configurationSchema":{"path":"workflow-nodes/phone/config.schema.json","sha256":"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"},"capability":"authentication.phone-confirmation","operation":"executeNode","permission":"authentication.phone.confirm","outcomes":["confirmed","expired"]}]}}}`)
	manifest, err := pluginv1.ParseManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	artifact := pluginv1.VerifiedArtifactManifest{Artifact: pluginv1.Artifact{PluginID: manifest.ID, Version: manifest.Version, Channel: "stable", SHA256: strings.Repeat("d", 64)}, Manifest: manifest}
	inventory, err := pluginv1.BuildPluginInventory(7, strings.Repeat("e", 64), []pluginv1.VerifiedArtifactManifest{artifact})
	if err != nil {
		t.Fatal(err)
	}
	index, err := pluginv1.BuildContributionIndex(inventory, []pluginv1.VerifiedArtifactManifest{artifact})
	if err != nil {
		t.Fatal(err)
	}
	registered, err := service.RegisterCatalog(ctx, repository, Actor{ID: "kernel", System: true}, index)
	if err != nil || registered.Templates != 1 || registered.Providers != 1 {
		t.Fatalf("registered=%+v err=%v", registered, err)
	}
	templateRecord, err := repository.Get(ctx, nodeTemplateKey("authentication.email-confirmation"))
	if err != nil {
		t.Fatal(err)
	}
	template, err := decodeDocument[nodeTemplateRecord](templateRecord)
	if err != nil || template.Owner.Ref.PluginID != manifest.ID || template.Owner.Ref.Version != manifest.Version || template.Generation != 7 {
		t.Fatalf("template=%+v err=%v", template, err)
	}
	if _, err := service.RegisterCatalog(ctx, repository, Actor{ID: "user"}, index); err == nil {
		t.Fatal("ordinary callers must not register node targets")
	}
}

func registerPortalFeature(t *testing.T, ctx context.Context, service *Service, repository Repository) workflowv1.FeatureDescriptor {
	t.Helper()
	raw := []byte(`{"id":"cn.vastplan.platform.configuration.portal-composer","name":"portal","description":"portal","version":"1.0.0","publisher":"vastplan","engines":{"backend":"^0.1"},"activation":["onStartup"],"entry":{"backend":"backend/main"},"contributes":{"backend":{"workflowFeatures":[{"id":"platform.portal.publication","contract":"1.0.0","resourceKind":"portal.publication","digestRequired":true,"prepare":{"capability":"platform.portal-composer","operation":"preparePortalPublication","permission":"platform.portal.compose"},"unboundPolicy":"direct","unboundActionId":"portal.release","actions":[{"id":"portal.release","capability":"platform.portal-composer","operation":"executePublicationRelease","permission":"platform.portal.publish","terminal":true,"compensatable":false}]}]}}}`)
	manifest, err := pluginv1.ParseManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	value := pluginv1.VerifiedArtifactManifest{Artifact: pluginv1.Artifact{PluginID: manifest.ID, Version: manifest.Version, Channel: "stable", SHA256: strings.Repeat("b", 64)}, Manifest: manifest}
	inventory, err := pluginv1.BuildPluginInventory(1, strings.Repeat("c", 64), []pluginv1.VerifiedArtifactManifest{value})
	if err != nil {
		t.Fatal(err)
	}
	index, err := pluginv1.BuildContributionIndex(inventory, []pluginv1.VerifiedArtifactManifest{value})
	if err != nil {
		t.Fatal(err)
	}
	if registered, err := service.RegisterCatalog(ctx, repository, Actor{ID: "kernel", System: true}, index); err != nil || registered.Features != 1 {
		t.Fatalf("registered=%+v err=%v", registered, err)
	}
	return workflowv1.FeatureDescriptor{ID: "platform.portal.publication", Contract: "1.0.0", ResourceKind: "portal.publication", DigestRequired: true, Prepare: &workflowv1.OperationDescriptor{Capability: "platform.portal-composer", Operation: "preparePortalPublication", Permission: "platform.portal.compose"}, UnboundPolicy: workflowv1.UnboundDirect, UnboundActionID: "portal.release", Actions: []workflowv1.ActionDescriptor{{ID: "portal.release", Capability: "platform.portal-composer", Operation: "executePublicationRelease", Permission: "platform.portal.publish", Terminal: true}}}
}
