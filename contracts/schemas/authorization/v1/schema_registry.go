package authorizationv1

import (
	"bytes"
	_ "embed"
	"fmt"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"

	commonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/common/v1"
)

const (
	typesSchemaURL     = "https://schemas.cdsoft.com.cn/vastplan/authorization/v1/vastplan.authorization-types.schema.json"
	profileSchemaURL   = "https://schemas.cdsoft.com.cn/vastplan/authorization/v1/vastplan.authorization-provider-profile.schema.json"
	storeSchemaURL     = "https://schemas.cdsoft.com.cn/vastplan/authorization/v1/vastplan.authorization-store.schema.json"
	engineSchemaURL    = "https://schemas.cdsoft.com.cn/vastplan/authorization/v1/vastplan.authorization-engine.schema.json"
	directorySchemaURL = "https://schemas.cdsoft.com.cn/vastplan/authorization/v1/vastplan.authorization-directory.schema.json"
	exchangeSchemaURL  = "https://schemas.cdsoft.com.cn/vastplan/authorization/v1/vastplan.authorization-exchange.schema.json"
)

//go:embed vastplan.authorization-types.schema.json
var typesSchemaJSON []byte

//go:embed vastplan.authorization-provider-profile.schema.json
var profileSchemaJSON []byte

//go:embed vastplan.authorization-ir.schema.json
var irSchemaJSON []byte

//go:embed vastplan.authorization-store.schema.json
var storeSchemaJSON []byte

//go:embed vastplan.authorization-engine.schema.json
var engineSchemaJSON []byte

//go:embed vastplan.authorization-directory.schema.json
var directorySchemaJSON []byte

//go:embed vastplan.authorization-exchange.schema.json
var exchangeSchemaJSON []byte

var (
	schemaOnce sync.Once
	compiler   *jsonschema.Compiler
	schemaErr  error
)

func compileSchemas() {
	compiler = jsonschema.NewCompiler()
	if err := commonv1.AddResources(compiler); err != nil {
		schemaErr = err
		return
	}
	resources := []struct {
		url string
		raw []byte
	}{
		{typesSchemaURL, typesSchemaJSON}, {profileSchemaURL, profileSchemaJSON},
		{IRSchemaURL, irSchemaJSON}, {storeSchemaURL, storeSchemaJSON},
		{engineSchemaURL, engineSchemaJSON}, {directorySchemaURL, directorySchemaJSON},
		{exchangeSchemaURL, exchangeSchemaJSON},
	}
	for _, resource := range resources {
		document, err := jsonschema.UnmarshalJSON(bytes.NewReader(resource.raw))
		if err != nil {
			schemaErr = fmt.Errorf("解析 Authorization Schema %s: %w", resource.url, err)
			return
		}
		if err := compiler.AddResource(resource.url, document); err != nil {
			schemaErr = fmt.Errorf("登记 Authorization Schema %s: %w", resource.url, err)
			return
		}
	}
}

func validateSchema(url string, raw []byte) error {
	schemaOnce.Do(compileSchemas)
	if schemaErr != nil {
		return schemaErr
	}
	schema, err := compiler.Compile(url)
	if err != nil {
		return fmt.Errorf("编译 Authorization Schema %s: %w", url, err)
	}
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("解析 Authorization JSON: %w", err)
	}
	if err := schema.Validate(document); err != nil {
		return fmt.Errorf("Authorization 消息不符合 Schema: %w", err)
	}
	return nil
}
