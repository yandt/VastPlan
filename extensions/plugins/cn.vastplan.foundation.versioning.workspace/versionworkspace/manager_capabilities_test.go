package versionworkspace

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	resourcev1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionresource/v1"
	workspacev1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionworkspace/v1"
)

const digestOnlyAdapterID = "version.resource.digest-only.v1"

type digestOnlyAdapter struct{ inner *JSONAdapter }

func newDigestOnlyAdapter() *digestOnlyAdapter {
	return &digestOnlyAdapter{inner: NewJSONAdapter()}
}

func (*digestOnlyAdapter) Descriptor() resourcev1.AdapterDescriptor {
	return resourcev1.AdapterDescriptor{
		Protocol: resourcev1.Protocol, ID: digestOnlyAdapterID, Version: "1.0.0",
		ContentKind: resourcev1.ContentJSON, SupportedModes: []string{resourcev1.ModeSnapshot}, DefaultMode: resourcev1.ModeSnapshot,
		MaxSnapshotBytes: 1 << 20, SecretPolicy: resourcev1.SecretPolicyCredentialRefsOnly,
		Capabilities: resourcev1.AdapterCapabilities{Normalize: true}, ConfigurationSchema: json.RawMessage(`{"type":"object","additionalProperties":false}`),
	}
}

func (a *digestOnlyAdapter) Normalize(ctx context.Context, request resourcev1.AdapterNormalizeRequest) (resourcev1.AdapterNormalizeResult, error) {
	return a.inner.Normalize(ctx, request)
}

type inconsistentDiffAdapter struct{ *digestOnlyAdapter }

func (a *inconsistentDiffAdapter) Descriptor() resourcev1.AdapterDescriptor {
	descriptor := a.digestOnlyAdapter.Descriptor()
	descriptor.Capabilities.Diff = true
	return descriptor
}

func TestCatalogRejectsCapabilityImplementationMismatch(t *testing.T) {
	catalog := NewCatalog()
	if err := catalog.RegisterAdapter(&inconsistentDiffAdapter{digestOnlyAdapter: newDigestOnlyAdapter()}); err == nil {
		t.Fatal("Adapter 的 diff 声明与可选接口实现不一致时必须拒绝")
	}
}

func TestManagerDescribesAndUsesDigestOnlyAdapter(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	catalog := NewCatalog()
	if err := catalog.RegisterAdapter(newDigestOnlyAdapter()); err != nil {
		t.Fatal(err)
	}
	profile := resourcev1.EnvironmentProfile{
		Protocol: resourcev1.Protocol, ID: "platform-development", Revision: 1,
		Bindings: []resourcev1.ResourceBinding{{
			ResourceType: "portal.configuration", Namespace: "portal.configuration", Adapter: digestOnlyAdapterID,
			AllowedModes: []string{resourcev1.ModeSnapshot}, DefaultMode: resourcev1.ModeSnapshot, ProjectionPolicy: resourcev1.ProjectionDomainHot,
		}},
		Limits: resourcev1.WorkspaceLimits{MaxSessionsPerTenant: 4, MaxLeaseSeconds: 3600, MaxSnapshotBytes: 1 << 20, MaxOverlayBytes: 1 << 20},
	}
	if err := catalog.RegisterEnvironment(profile); err != nil {
		t.Fatal(err)
	}
	sequence := 0
	manager, err := NewManager(catalog, ManagerOptions{Now: func() time.Time { return now }, NewSessionID: func() (string, error) {
		sequence++
		return fmt.Sprintf("ws_%016d", sequence), nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	scope := Scope{TenantID: "tenant-a", ActorID: "plugin:portal"}
	resource := resourcev1.ResourceKey{Type: "portal.configuration", ID: "portal-main"}
	description, err := manager.DescribeResource(scope, workspacev1.DescribeResourceRequest{EnvironmentID: profile.ID, Resource: resource})
	if err != nil || description.Capabilities.Diff || !description.Capabilities.Normalize || description.Resolution.Adapter != digestOnlyAdapterID {
		t.Fatalf("资源能力描述错误: %+v err=%v", description, err)
	}
	ledger := newMemoryLedger()
	firstSession, err := manager.Open(context.Background(), scope, ledger, workspacev1.OpenRequest{EnvironmentID: profile.ID, Resource: resource, LeaseSeconds: 60})
	if err != nil {
		t.Fatal(err)
	}
	firstSession = writeJSON(t, manager, scope, firstSession, `{"revision":1}`)
	changes, err := manager.Changes(context.Background(), scope, workspacev1.SessionRequest{SessionID: firstSession.ID})
	if err != nil || !changes.Dirty || changes.DiffAvailable || len(changes.ChangedPaths) != 0 || changes.Summary.Total != 0 {
		t.Fatalf("无 diff Adapter 的变化语义错误: %+v err=%v", changes, err)
	}
	first, err := manager.Commit(context.Background(), scope, ledger, workspacev1.CommitRequest{
		SessionID: firstSession.ID, ExpectedRevision: firstSession.Revision, OperationID: "portal-publication:digest-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	secondSession, err := manager.Open(context.Background(), scope, ledger, workspacev1.OpenRequest{
		EnvironmentID: profile.ID, Resource: resource, BaseRef: &first.Version.Ref, LeaseSeconds: 60,
	})
	if err != nil {
		t.Fatal(err)
	}
	secondSession = writeJSON(t, manager, scope, secondSession, `{"revision":2}`)
	second, err := manager.Commit(context.Background(), scope, ledger, workspacev1.CommitRequest{
		SessionID: secondSession.ID, ExpectedRevision: secondSession.Revision, OperationID: "portal-publication:digest-2",
	})
	if err != nil {
		t.Fatal(err)
	}
	compared, err := manager.CompareCommitted(context.Background(), scope, ledger, workspacev1.CompareCommittedRequest{
		EnvironmentID: profile.ID, EnvironmentDigest: second.Session.EnvironmentDigest, Resource: resource,
		Left: first.Version.Ref, Right: second.Version.Ref,
	})
	if err != nil || !compared.Dirty || compared.DiffAvailable || len(compared.ChangedPaths) != 0 || compared.Summary.Total != 0 {
		t.Fatalf("无 diff Adapter 的已提交比较语义错误: %+v err=%v", compared, err)
	}
}
