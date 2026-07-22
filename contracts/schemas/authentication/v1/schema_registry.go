package authenticationv1

import (
	"bytes"
	_ "embed"
	"fmt"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"

	commonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/common/v1"
	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
)

const (
	TypesSchemaURL     = "https://schemas.cdsoft.com.cn/vastplan/authentication/v1/vastplan.authentication-types.schema.json"
	MethodSchemaURL    = "https://schemas.cdsoft.com.cn/vastplan/authentication/v1/vastplan.authentication-method.schema.json"
	AssertionSchemaURL = "https://schemas.cdsoft.com.cn/vastplan/authentication/v1/vastplan.authentication-assertion.schema.json"
	BrokerSchemaURL    = "https://schemas.cdsoft.com.cn/vastplan/authentication/v1/vastplan.authentication-broker.schema.json"
	AccessSchemaURL    = "https://schemas.cdsoft.com.cn/vastplan/authentication/v1/vastplan.access-profile.schema.json"
	ProviderSchemaURL  = "https://schemas.cdsoft.com.cn/vastplan/authentication/v1/vastplan.authentication-provider.schema.json"

	MaxMethodMessageBytes   = 64 << 10
	MaxAssertionBytes       = 64 << 10
	MaxAccessProfileBytes   = 1 << 20
	MaxAccessCatalogBytes   = 16 << 20
	MaxProviderProfileBytes = 1 << 20
	MaxProviderCatalogBytes = 16 << 20
)

//go:embed vastplan.authentication-types.schema.json
var typesSchemaJSON []byte

//go:embed vastplan.authentication-method.schema.json
var methodSchemaJSON []byte

//go:embed vastplan.authentication-assertion.schema.json
var assertionSchemaJSON []byte

//go:embed vastplan.authentication-broker.schema.json
var brokerSchemaJSON []byte

//go:embed vastplan.access-profile.schema.json
var accessSchemaJSON []byte

//go:embed vastplan.authentication-provider.schema.json
var providerSchemaJSON []byte

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
	if err := compositioncommonv1.AddResources(compiler); err != nil {
		schemaErr = err
		return
	}
	resources := []struct {
		url string
		raw []byte
	}{
		{TypesSchemaURL, typesSchemaJSON}, {MethodSchemaURL, methodSchemaJSON},
		{AssertionSchemaURL, assertionSchemaJSON}, {BrokerSchemaURL, brokerSchemaJSON},
		{AccessSchemaURL, accessSchemaJSON},
		{ProviderSchemaURL, providerSchemaJSON},
	}
	for _, resource := range resources {
		document, err := jsonschema.UnmarshalJSON(bytes.NewReader(resource.raw))
		if err != nil {
			schemaErr = fmt.Errorf("解析 Authentication Schema %s: %w", resource.url, err)
			return
		}
		if err := compiler.AddResource(resource.url, document); err != nil {
			schemaErr = fmt.Errorf("登记 Authentication Schema %s: %w", resource.url, err)
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
		return fmt.Errorf("编译 Authentication Schema %s: %w", url, err)
	}
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("解析 Authentication JSON: %w", err)
	}
	if err := schema.Validate(document); err != nil {
		return fmt.Errorf("Authentication 消息不符合 Schema: %w", err)
	}
	return nil
}
