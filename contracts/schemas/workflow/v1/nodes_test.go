package workflowv1

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestNodeTemplateAndProviderDescriptorsAreContentBound(t *testing.T) {
	document := ArtifactDocumentRef{Path: "workflow-nodes/email/config.schema.json", SHA256: strings.Repeat("a", 64)}
	template := NodeTemplateDescriptor{ID: "authentication.email-confirmation", Contract: "1.0.0", Title: "Email confirmation", CompilerContract: NodeTemplateProtocol, ConfigurationSchema: document, Expansion: ArtifactDocumentRef{Path: "workflow-nodes/email/expansion.json", SHA256: strings.Repeat("b", 64)}, Outcomes: []string{"confirmed", "expired"}}
	if err := ValidateNodeTemplate(template); err != nil {
		t.Fatal(err)
	}
	provider := NodeProviderDescriptor{ID: "authentication.phone-confirmation", Contract: "1.0.0", Title: "Phone confirmation", EffectContract: NodeEffectProtocol, ConfigurationSchema: document, Capability: "authentication.phone-confirmation", Operation: "executeNode", Permission: "authentication.phone.confirm", Outcomes: []string{"confirmed", "expired"}}
	if err := ValidateNodeProvider(provider); err != nil {
		t.Fatal(err)
	}
	provider.Capability = "https://example.invalid"
	if err := ValidateNodeProvider(provider); err == nil {
		t.Fatal("arbitrary provider target must be rejected")
	}
}

func TestNodeEffectsAreClosedAndOutcomeBound(t *testing.T) {
	outcomes := []string{"confirmed", "expired"}
	if err := ValidateNodeEffect(NodeEffect{Kind: NodeEffectComplete, Outcome: "confirmed", Facts: json.RawMessage(`{"verified":true}`)}, outcomes); err != nil {
		t.Fatal(err)
	}
	deadline := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	if err := ValidateNodeEffect(NodeEffect{Kind: NodeEffectWait, Wait: &WaitEffect{EventContract: "authentication.email.confirmed", CorrelationID: "registration/42", Deadline: deadline}}, outcomes); err != nil {
		t.Fatal(err)
	}
	if err := ValidateNodeEffect(NodeEffect{Kind: NodeEffectComplete, Outcome: "forged"}, outcomes); err == nil {
		t.Fatal("undeclared outcome must be rejected")
	}
	if err := ValidateNodeEffect(NodeEffect{Kind: NodeEffectWait, Wait: &WaitEffect{EventContract: "authentication.email.confirmed", CorrelationID: "registration/42", Deadline: deadline}, Outcome: "confirmed"}, outcomes); err == nil {
		t.Fatal("wait effect cannot smuggle a completion outcome")
	}
}
