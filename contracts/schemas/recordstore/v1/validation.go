package recordstorev1

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

//go:embed vastplan.record-store.schema.json
var schemaJSON []byte

var schemaState struct {
	sync.Once
	definitions map[string]*jsonschema.Schema
	err         error
}

var requestDefinitions = map[string]string{
	OperationSyncModels: "syncModelsRequest", OperationCreate: "createRequest", OperationGet: "getRequest",
	OperationList: "listRequest", OperationUpdate: "updateRequest", OperationDelete: "deleteRequest",
	OperationBatch: "batchRequest", OperationBegin: "beginRequest", OperationCommit: "endRequest",
	OperationRollback: "endRequest", OperationAppendOutbox: "appendOutboxRequest",
	OperationSchemaPlan: "schemaRequest", OperationSchemaApply: "schemaRequest", OperationSchemaStatus: "schemaRequest",
}

func ParseRequest(operation string, raw []byte) (any, error) {
	definition, ok := requestDefinitions[operation]
	if !ok {
		return nil, fmt.Errorf("不支持的 Record Store 操作 %q", operation)
	}
	if err := validateDefinition(definition, raw); err != nil {
		return nil, err
	}
	var target any
	switch operation {
	case OperationSyncModels:
		target = &SyncModelsRequest{}
	case OperationCreate:
		target = &CreateRequest{}
	case OperationGet:
		target = &GetRequest{}
	case OperationList:
		target = &ListRequest{}
	case OperationUpdate:
		target = &UpdateRequest{}
	case OperationDelete:
		target = &DeleteRequest{}
	case OperationBatch:
		target = &BatchRequest{}
	case OperationBegin:
		target = &BeginRequest{}
	case OperationCommit, OperationRollback:
		target = &EndRequest{}
	case OperationAppendOutbox:
		target = &AppendOutboxRequest{}
	case OperationSchemaPlan, OperationSchemaApply, OperationSchemaStatus:
		target = &SchemaRequest{}
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return nil, err
	}
	if request, ok := target.(*SyncModelsRequest); ok {
		if err := ValidateSchemaActivation(request.SchemaActivation); err != nil {
			return nil, err
		}
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, errors.New("Record Store 请求只能包含一个 JSON 文档")
	}
	return target, nil
}

func validateDefinition(definition string, raw []byte) error {
	schemaState.Do(func() {
		compiler := jsonschema.NewCompiler()
		document, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemaJSON))
		if err == nil {
			err = compiler.AddResource(SchemaURL, document)
		}
		if err == nil {
			schemaState.definitions = map[string]*jsonschema.Schema{}
			for _, name := range requestDefinitions {
				if _, exists := schemaState.definitions[name]; exists {
					continue
				}
				schemaState.definitions[name], err = compiler.Compile(SchemaURL + "#/$defs/" + name)
				if err != nil {
					break
				}
			}
		}
		schemaState.err = err
	})
	if schemaState.err != nil {
		return fmt.Errorf("编译 Record Store Schema: %w", schemaState.err)
	}
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("解析 Record Store JSON: %w", err)
	}
	if err := schemaState.definitions[definition].Validate(instance); err != nil {
		return fmt.Errorf("Record Store %s 不符合 Schema: %w", definition, err)
	}
	return nil
}

// AddResources registers the canonical Record Store schema for contracts that
// embed host-owned schema activation evidence.
func AddResources(compiler *jsonschema.Compiler) error {
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemaJSON))
	if err != nil {
		return fmt.Errorf("解析 Record Store Schema: %w", err)
	}
	if err := compiler.AddResource(SchemaURL, document); err != nil {
		return fmt.Errorf("登记 Record Store Schema: %w", err)
	}
	return nil
}
