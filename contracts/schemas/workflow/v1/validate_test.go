package workflowv1

import "testing"

func TestValidateDefinitionAcceptsGovernedFlow(t *testing.T) {
	feature := testFeature()
	definition := Definition{ID: "platform.portal.release", Revision: 1, FeatureID: feature.ID, EntryNodeID: "review", Nodes: []Node{
		{ID: "review", Type: CoreNode(NodeManual), Title: "Review", Roles: []string{"portal.approver"}, Outcomes: map[string]string{"approved": "release", "rejected": "rejected"}},
		{ID: "release", Type: CoreNode(NodeAction), ActionID: "portal.release", Next: "done"},
		{ID: "done", Type: CoreNode(NodeEnd), Result: ResultSucceeded},
		{ID: "rejected", Type: CoreNode(NodeEnd), Result: ResultRejected},
	}}
	if err := ValidateFeature(feature); err != nil {
		t.Fatal(err)
	}
	if err := ValidateDefinition(definition, feature); err != nil {
		t.Fatal(err)
	}
	if digest, err := DefinitionDigest(definition); err != nil || len(digest) != 64 {
		t.Fatalf("digest=%q err=%v", digest, err)
	}
}

func TestValidateDefinitionRejectsUnknownActionCycleAndUnreachableNode(t *testing.T) {
	feature := testFeature()
	for name, definition := range map[string]Definition{
		"unknown action": {ID: "platform.portal.release", Revision: 1, FeatureID: feature.ID, EntryNodeID: "run", Nodes: []Node{{ID: "run", Type: CoreNode(NodeAction), ActionID: "portal.unknown", Next: "done"}, {ID: "done", Type: CoreNode(NodeEnd), Result: ResultSucceeded}}},
		"cycle":          {ID: "platform.portal.release", Revision: 1, FeatureID: feature.ID, EntryNodeID: "run", Nodes: []Node{{ID: "run", Type: CoreNode(NodeAction), ActionID: "portal.release", Next: "run"}}},
		"unreachable":    {ID: "platform.portal.release", Revision: 1, FeatureID: feature.ID, EntryNodeID: "done", Nodes: []Node{{ID: "done", Type: CoreNode(NodeEnd), Result: ResultSucceeded}, {ID: "unused", Type: CoreNode(NodeEnd), Result: ResultRejected}}},
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateDefinition(definition, feature); err == nil {
				t.Fatal("invalid definition must be rejected")
			}
		})
	}
}

func TestValidateFeatureRequiresExplicitTerminalDirectAction(t *testing.T) {
	feature := testFeature()
	feature.UnboundPolicy = UnboundDirect
	if err := ValidateFeature(feature); err == nil {
		t.Fatal("direct policy without an action must be rejected")
	}
	feature.UnboundActionID = "portal.release"
	if err := ValidateFeature(feature); err != nil {
		t.Fatal(err)
	}
	feature.Actions[0].Terminal = false
	if err := ValidateFeature(feature); err == nil {
		t.Fatal("non-terminal direct action must be rejected")
	}
}

func testFeature() FeatureDescriptor {
	return FeatureDescriptor{ID: "platform.portal.publication", Contract: "1.0.0", ResourceKind: "portal.publication", DigestRequired: true, Actions: []ActionDescriptor{{ID: "portal.release", Capability: "platform.portal-composer", Operation: "executePublicationRelease", Permission: "platform.portal.publish", Terminal: true}}}
}
