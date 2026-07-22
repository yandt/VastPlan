package apiv1

import (
	"bytes"
	_ "embed"
	"fmt"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

//go:embed vastplan.api-exposure.schema.json
var schemaJSON []byte

var (
	schemaOnce sync.Once
	compiler   *jsonschema.Compiler
	schemaErr  error
)

func compileSchema() {
	compiler = jsonschema.NewCompiler()
	if err := AddResources(compiler); err != nil {
		schemaErr = err
	}
}

// AddResources lets other contract packages, especially the plugin manifest
// compiler, reference the same embedded API definitions without copying them.
func AddResources(target *jsonschema.Compiler) error {
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemaJSON))
	if err != nil {
		return fmt.Errorf("解析 API Exposure Schema: %w", err)
	}
	if err := target.AddResource(SchemaURL, document); err != nil {
		return fmt.Errorf("登记 API Exposure Schema: %w", err)
	}
	return nil
}

func validateDefinition(definition string, value any) error {
	schemaOnce.Do(compileSchema)
	if schemaErr != nil {
		return schemaErr
	}
	schema, err := compiler.Compile(SchemaURL + "#/$defs/" + definition)
	if err != nil {
		return fmt.Errorf("编译 API Exposure %s Schema: %w", definition, err)
	}
	if err := schema.Validate(value); err != nil {
		return fmt.Errorf("API Exposure %s 不符合 Schema: %w", definition, err)
	}
	return nil
}
