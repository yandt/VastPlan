package versionstagingv1

import (
	"bytes"
	_ "embed"
	"os"
	"testing"

	versionresourcev1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionresource/v1"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

//go:embed vastplan.version-staging.schema.json
var stagingSchemaJSON []byte

func TestStagingSchemaCompilesWithVersionResourceContract(t *testing.T) {
	resourceSchema, err := os.ReadFile("../../versionresource/v1/vastplan.version-resource.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	compiler := jsonschema.NewCompiler()
	for uri, raw := range map[string][]byte{SchemaURL: stagingSchemaJSON, versionresourcev1.SchemaURL: resourceSchema} {
		document, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
		if err != nil {
			t.Fatalf("解析 %s: %v", uri, err)
		}
		if err := compiler.AddResource(uri, document); err != nil {
			t.Fatalf("登记 %s: %v", uri, err)
		}
	}
	for _, definition := range []string{"beginUploadRequest", "uploadLease", "uploadStatusRequest", "uploadRevisionRequest", "renewUploadRequest", "contentDescriptor", "uploadStatusResult"} {
		if _, err := compiler.Compile(SchemaURL + "#/$defs/" + definition); err != nil {
			t.Fatalf("编译 Staging Schema %s: %v", definition, err)
		}
	}
}
