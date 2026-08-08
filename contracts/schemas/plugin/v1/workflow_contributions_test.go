package pluginv1

import (
	"encoding/json"
	"testing"
)

func TestWorkflowFeatureIsSignedDeclarativeContribution(t *testing.T) {
	raw := []byte(`{"id":"com.example.portal","name":"portal","description":"portal","version":"1.0.0","publisher":"example","engines":{"backend":"^1.0"},"activation":["onStartup"],"entry":{"backend":"backend/main"},"contributes":{"backend":{"workflowFeatures":[{"id":"example.portal.publication","contract":"1.0.0","resourceKind":"example.portal.publication","digestRequired":true,"unboundPolicy":"direct","unboundActionId":"example.portal.release","actions":[{"id":"example.portal.release","capability":"example.portal-composer","operation":"executeRelease","permission":"example.portal.publish","terminal":true,"compensatable":false}]}]}}}`)
	manifest, err := ParseManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	if runtime, err := BackendRuntimeContributions(manifest); err != nil || len(runtime) != 0 {
		t.Fatalf("workflow feature must remain declarative: runtime=%+v err=%v", runtime, err)
	}
	owner := PluginArtifactIdentity{Ref: ArtifactRef{PluginID: manifest.ID, Version: manifest.Version, Channel: "stable"}, SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Publisher: manifest.Publisher}
	indexed, err := manifestContributions(owner, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if len(indexed) != 1 || indexed[0].Kind != "backend.workflowFeatures" || indexed[0].Owner.Ref.PluginID != manifest.ID {
		t.Fatalf("unexpected contribution inventory: %+v", indexed)
	}
	var descriptor map[string]any
	if err := json.Unmarshal(indexed[0].Descriptor, &descriptor); err != nil || descriptor["id"] != "example.portal.publication" {
		t.Fatalf("descriptor was not preserved: %+v err=%v", descriptor, err)
	}
}

func TestWorkflowFeatureRejectsArbitraryActionTargetFields(t *testing.T) {
	raw := []byte(`{"id":"com.example.portal","name":"portal","description":"portal","version":"1.0.0","publisher":"example","engines":{"backend":"^1.0"},"activation":["onStartup"],"entry":{"backend":"backend/main"},"contributes":{"backend":{"workflowFeatures":[{"id":"example.portal.publication","contract":"1.0.0","resourceKind":"example.portal.publication","digestRequired":true,"actions":[{"id":"example.portal.release","capability":"example.portal-composer","operation":"executeRelease","permission":"example.portal.publish","terminal":true,"compensatable":false,"url":"https://example.invalid"}]}]}}}`)
	if _, err := ParseManifest(raw); err == nil {
		t.Fatal("arbitrary workflow action target fields must be rejected")
	}
}

func TestWorkflowNodeExtensionsRemainSignedDeclarativeContributions(t *testing.T) {
	raw := []byte(`{"id":"com.example.authentication","name":"authentication","description":"authentication","version":"1.0.0","publisher":"example","engines":{"backend":"^1.0"},"activation":["onStartup"],"entry":{"backend":"backend/main"},"contributes":{"backend":{"workflowNodeTemplates":[{"id":"authentication.email-confirmation","contract":"1.0.0","title":"Email confirmation","compilerContract":"workflow.node-template.v1","configurationSchema":{"path":"workflow-nodes/email/config.schema.json","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"expansion":{"path":"workflow-nodes/email/expansion.json","sha256":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},"outcomes":["confirmed","expired"]}],"workflowNodeProviders":[{"id":"authentication.phone-confirmation","contract":"1.0.0","title":"Phone confirmation","effectContract":"workflow.node-effect.v1","configurationSchema":{"path":"workflow-nodes/phone/config.schema.json","sha256":"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"},"capability":"authentication.phone-confirmation","operation":"executeNode","permission":"authentication.phone.confirm","outcomes":["confirmed","expired"]}]}}}`)
	manifest, err := ParseManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	if runtime, err := BackendRuntimeContributions(manifest); err != nil || len(runtime) != 0 {
		t.Fatalf("node catalogs must remain declarative: runtime=%+v err=%v", runtime, err)
	}
	owner := PluginArtifactIdentity{Ref: ArtifactRef{PluginID: manifest.ID, Version: manifest.Version, Channel: "stable"}, SHA256: "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd", Publisher: manifest.Publisher}
	indexed, err := manifestContributions(owner, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if len(indexed) != 2 || indexed[0].Owner.Ref.PluginID != manifest.ID || indexed[1].Owner.Ref.PluginID != manifest.ID {
		t.Fatalf("unexpected node contribution inventory: %+v", indexed)
	}
	kinds := map[string]bool{indexed[0].Kind: true, indexed[1].Kind: true}
	if !kinds["backend.workflowNodeTemplates"] || !kinds["backend.workflowNodeProviders"] {
		t.Fatalf("unexpected node contribution kinds: %+v", kinds)
	}
}
