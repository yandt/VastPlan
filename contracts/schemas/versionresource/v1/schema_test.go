package versionresourcev1

import (
	"bytes"
	_ "embed"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

//go:embed vastplan.version-resource.schema.json
var resourceSchemaJSON []byte

func TestResourceSchemaCompilesAllPublicDefinitions(t *testing.T) {
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(resourceSchemaJSON))
	if err != nil {
		t.Fatal(err)
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource(SchemaURL, document); err != nil {
		t.Fatal(err)
	}
	for _, definition := range []string{
		"resourceKey", "fileEntry", "snapshot", "adapterCapabilities", "adapterDescriptor",
		"adapterDescribeRequest", "adapterDescribeResult", "adapterNormalizeRequest", "adapterNormalizeResult",
		"changeSummary", "adapterDiffRequest", "adapterDiffResult", "adapterMaterializeRequest", "materializationRef", "adapterMaterializeResult",
		"resourceBinding", "environmentProfile",
	} {
		if _, err := compiler.Compile(SchemaURL + "#/$defs/" + definition); err != nil {
			t.Fatalf("编译 Resource Schema %s: %v", definition, err)
		}
	}
}
