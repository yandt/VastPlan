package sharedstatesqlv1

import (
	"bytes"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"cdsoft.com.cn/VastPlan/extensions/libraries/go/sharedstate"
)

const schemaURL = "https://schemas.cdsoft.com.cn/vastplan/sharedstatesql/v1/vastplan.shared-state-sql.schema.json"

//go:embed vastplan.shared-state-sql.schema.json
var schemaJSON []byte

var compiled struct {
	sync.Once
	definitions map[string]*jsonschema.Schema
	err         error
}

var definitions = map[string]string{OperationGet: "keyRequest", OperationCreate: "writeRequest", OperationUpdate: "writeRequest", OperationDelete: "deleteRequest", OperationList: "listRequest"}

func ParseRequest(operation string, raw []byte) (any, error) {
	definition, ok := definitions[operation]
	if !ok {
		return nil, fmt.Errorf("不支持的 SQL Shared State 操作 %q", operation)
	}
	if err := validate(definition, raw); err != nil {
		return nil, err
	}
	var target any
	switch operation {
	case OperationGet:
		target = &KeyRequest{}
	case OperationCreate, OperationUpdate:
		target = &WriteRequest{}
	case OperationDelete:
		target = &DeleteRequest{}
	case OperationList:
		target = &ListRequest{}
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, errors.New("SQL Shared State 请求只能包含一个 JSON 文档")
	}
	if err := validateSemantics(operation, target); err != nil {
		return nil, err
	}
	return target, nil
}

func validateSemantics(operation string, request any) error {
	var wire Scope
	var key string
	switch value := request.(type) {
	case *KeyRequest:
		wire, key = value.Scope, value.Key
	case *WriteRequest:
		wire, key = value.Scope, value.Key
		raw, err := base64.StdEncoding.DecodeString(value.ValueBase64)
		if err != nil || sharedstate.ValidateValue(raw) != nil {
			return errors.New("SQL Shared State value 无效")
		}
		if operation == OperationUpdate && value.ExpectedRevision == 0 {
			return errors.New("SQL Shared State update 缺少 revision")
		}
	case *DeleteRequest:
		wire, key = value.Scope, value.Key
		if value.ExpectedRevision == 0 {
			return errors.New("SQL Shared State delete 缺少 revision")
		}
	case *ListRequest:
		wire = value.Scope
		if err := sharedstate.ValidateList(value.Prefix, value.Limit, value.Cursor); err != nil {
			return err
		}
	}
	scope := sharedstate.Scope{Kind: sharedstate.ScopeKind(wire.Kind), TenantID: wire.TenantID, PluginID: wire.PluginID, RuntimeScope: wire.RuntimeScope, Namespace: wire.Namespace}
	if err := scope.Validate(); err != nil {
		return err
	}
	if key != "" {
		return sharedstate.ValidateKey(key)
	}
	return nil
}

func validate(definition string, raw []byte) error {
	compiled.Do(func() {
		compiler := jsonschema.NewCompiler()
		document, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemaJSON))
		if err == nil {
			err = compiler.AddResource(schemaURL, document)
		}
		compiled.definitions = map[string]*jsonschema.Schema{}
		if err == nil {
			for _, name := range definitions {
				if compiled.definitions[name] != nil {
					continue
				}
				compiled.definitions[name], err = compiler.Compile(schemaURL + "#/$defs/" + name)
				if err != nil {
					break
				}
			}
		}
		compiled.err = err
	})
	if compiled.err != nil {
		return compiled.err
	}
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return err
	}
	return compiled.definitions[definition].Validate(instance)
}
