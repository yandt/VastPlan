package workfloworchestrator

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/extpoint"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	workflowv1 "cdsoft.com.cn/VastPlan/contracts/schemas/workflow/v1"
)

type governedOperationHost struct{ targets []string }

func (h *governedOperationHost) Call(_ context.Context, target *contractv1.CallTarget, _ *contractv1.CallContext, _ []byte) (*contractv1.CallResult, []byte, error) {
	h.targets = append(h.targets, target.GetCapability()+"/"+target.GetOperation())
	if target.GetExtensionPoint() != extpoint.ToolPackage {
		return nil, nil, ErrInvalidState
	}
	if target.GetOperation() == "preparePortalPublication" {
		raw, _ := json.Marshal(workflowv1.PreparedResource{Resource: workflowv1.ResourceRef{Kind: "portal.publication", ID: "42"}, Digest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Revision: 3})
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
	}
	if target.GetOperation() == "executePublicationRelease" {
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, []byte(`{"releaseId":9}`), nil
	}
	return nil, nil, ErrInvalidState
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
	if len(contributions) != 1 || contributions[0].ID != Capability {
		t.Fatalf("contributions=%+v", contributions)
	}
	var signed, runtime any
	if json.Unmarshal(contributions[0].Descriptor, &signed) != nil || json.Unmarshal(Descriptor(), &runtime) != nil || !reflect.DeepEqual(signed, runtime) {
		t.Fatalf("runtime descriptor differs from signed manifest\nsigned=%s\nruntime=%s", contributions[0].Descriptor, Descriptor())
	}
	if PluginVersion != manifest.Version {
		t.Fatalf("runtime version=%s manifest=%s", PluginVersion, manifest.Version)
	}
}

func TestGovernUsesSignedPrepareTargetAndHidesStartFromFeaturePlugin(t *testing.T) {
	ctx := context.Background()
	repository := newMemoryRepository()
	service := New()
	registerPortalFeature(t, ctx, service, repository)
	host := &governedOperationHost{}
	raw, err := govern(ctx, host, &contractv1.CallContext{TenantId: "tenant", Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_USER, Id: "author"}}, repository, service, Actor{ID: "author"}, workflowv1.GovernedOperationRequest{ServiceID: "portal-service", FeatureID: "platform.portal.publication", PreparePayload: json.RawMessage(`{"portalId":"operations"}`), IdempotencyKey: "portal-submit-42"})
	if err != nil {
		t.Fatal(err)
	}
	var result workflowv1.GovernedOperationResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	if result.Instance.Mode != workflowv1.ExecutionDirect || result.Instance.Status != workflowv1.InstanceSucceeded {
		t.Fatalf("result=%+v", result)
	}
	expected := []string{"platform.portal-composer/preparePortalPublication", "platform.portal-composer/executePublicationRelease"}
	if !reflect.DeepEqual(host.targets, expected) {
		t.Fatalf("targets=%v expected=%v", host.targets, expected)
	}
}
