package versionworkspacev1

import (
	"bytes"
	_ "embed"
	"os"
	"testing"

	versioningv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versioning/v1"
	versionresourcev1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionresource/v1"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

//go:embed vastplan.version-workspace.schema.json
var workspaceSchemaJSON []byte

func TestWorkspaceSchemaCompilesWithExactExternalContracts(t *testing.T) {
	resourceSchema, err := os.ReadFile("../../versionresource/v1/vastplan.version-resource.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	ledgerSchema, err := os.ReadFile("../../versioning/v1/vastplan.version-ledger.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	compiler := jsonschema.NewCompiler()
	for uri, raw := range map[string][]byte{
		SchemaURL: workspaceSchemaJSON, versionresourcev1.SchemaURL: resourceSchema, versioningv1.SchemaURL: ledgerSchema,
	} {
		document, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
		if err != nil {
			t.Fatalf("解析 %s: %v", uri, err)
		}
		if err := compiler.AddResource(uri, document); err != nil {
			t.Fatalf("登记 %s: %v", uri, err)
		}
	}
	for _, definition := range []string{
		"session", "openRequest", "sessionRequest", "revisionRequest", "writeSnapshotRequest", "commitRequest", "committedRequest", "compareCommittedRequest", "renewRequest",
		"sessionResult", "snapshotResult", "changeSummary", "changesResult", "resourceResolution", "committedSnapshotResult", "compareCommittedResult", "commitResult",
	} {
		if _, err := compiler.Compile(SchemaURL + "#/$defs/" + definition); err != nil {
			t.Fatalf("编译 Workspace Schema %s: %v", definition, err)
		}
	}
}
