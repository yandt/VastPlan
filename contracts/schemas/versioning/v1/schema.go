package versioningv1

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

//go:embed vastplan.version-ledger.schema.json
var schemaJSON []byte

var (
	schemaOnce sync.Once
	schemas    map[string]*jsonschema.Schema
	schemaErr  error
)

var requestDefinitions = map[string]string{
	OperationProviders: "providerListRequest", OperationPutVersion: "putVersionRequest",
	OperationGetVersion: "getVersionRequest", OperationListHistory: "listHistoryRequest",
	OperationGetHead: "getHeadRequest", OperationMoveHead: "moveHeadRequest",
}

var resultDefinitions = map[string]string{
	OperationProviders: "providerListResult", OperationPutVersion: "putVersionResult",
	OperationGetVersion: "getVersionResult", OperationListHistory: "listHistoryResult",
	OperationGetHead: "getHeadResult", OperationMoveHead: "moveHeadResult",
}

var providerRequestDefinitions = map[string]string{
	ProviderOperationDescribe: "providerDescribeRequest", ProviderOperationPutVersion: "providerPutVersionRequest",
	ProviderOperationGetVersion: "getVersionRequest", ProviderOperationHistory: "listHistoryRequest",
	ProviderOperationGetHead: "getHeadRequest", ProviderOperationMoveHead: "moveHeadRequest",
}

var providerResultDefinitions = map[string]string{
	ProviderOperationDescribe: "providerDescribeResult", ProviderOperationPutVersion: "putVersionResult",
	ProviderOperationGetVersion: "getVersionResult", ProviderOperationHistory: "listHistoryResult",
	ProviderOperationGetHead: "getHeadResult", ProviderOperationMoveHead: "moveHeadResult",
}

func compileSchemas() {
	compiler := jsonschema.NewCompiler()
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemaJSON))
	if err != nil {
		schemaErr = fmt.Errorf("解析 Version Ledger Schema: %w", err)
		return
	}
	if err := compiler.AddResource(SchemaURL, document); err != nil {
		schemaErr = fmt.Errorf("登记 Version Ledger Schema: %w", err)
		return
	}
	names := []string{
		"streamKey", "versionRef", "versionRecord", "head", "providerDescriptor", "providerVersionCandidate",
		"providerListRequest", "providerListResult", "putVersionRequest", "putVersionResult",
		"getVersionRequest", "getVersionResult", "listHistoryRequest", "listHistoryResult",
		"getHeadRequest", "getHeadResult", "moveHeadRequest", "moveHeadResult",
		"providerDescribeRequest", "providerDescribeResult", "providerPutVersionRequest",
	}
	schemas = make(map[string]*jsonschema.Schema, len(names))
	for _, name := range names {
		schemas[name], err = compiler.Compile(SchemaURL + "#/$defs/" + name)
		if err != nil {
			schemaErr = fmt.Errorf("编译 Version Ledger Schema %s: %w", name, err)
			return
		}
	}
}

func validateDefinition(name string, raw []byte) error {
	schemaOnce.Do(compileSchemas)
	if schemaErr != nil {
		return schemaErr
	}
	schema := schemas[name]
	if schema == nil {
		return fmt.Errorf("未知 Version Ledger Schema 定义 %q", name)
	}
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("解析 Version Ledger JSON: %w", err)
	}
	if err := schema.Validate(document); err != nil {
		return fmt.Errorf("Version Ledger %s 不符合 Schema: %w", name, err)
	}
	return nil
}

func decodeStrict(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("Version Ledger 只能包含一个 JSON 文档")
	}
	return nil
}

func decodeDefinition(name string, raw []byte, target any) error {
	if err := rejectDuplicateJSONKeys(raw); err != nil {
		return err
	}
	if err := validateDefinition(name, raw); err != nil {
		return err
	}
	return decodeStrict(raw, target)
}
