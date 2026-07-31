package versioncontentv1

import (
	"bytes"
	_ "embed"
	"os"
	"testing"

	versioningv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versioning/v1"
	resourcev1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionresource/v1"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

//go:embed vastplan.version-content.schema.json
var contentSchemaJSON []byte

func TestContentSchemaCompilesWithReferencedContracts(t *testing.T) {
	ledgerSchema, err := os.ReadFile("../../versioning/v1/vastplan.version-ledger.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	resourceSchema, err := os.ReadFile("../../versionresource/v1/vastplan.version-resource.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	compiler := jsonschema.NewCompiler()
	for uri, raw := range map[string][]byte{SchemaURL: contentSchemaJSON, versioningv1.SchemaURL: ledgerSchema, resourcev1.SchemaURL: resourceSchema} {
		document, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		if err := compiler.AddResource(uri, document); err != nil {
			t.Fatal(err)
		}
	}
	for _, definition := range []string{"contentEntry", "protectedContentEntry", "prepareRequest", "statusRequest", "confirmRequest", "abortRequest", "protection", "protectionResult"} {
		if _, err := compiler.Compile(SchemaURL + "#/$defs/" + definition); err != nil {
			t.Fatalf("编译 Content Reference Schema %s: %v", definition, err)
		}
	}
}
