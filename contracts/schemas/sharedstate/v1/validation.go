package sharedstatev1

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
)

//go:embed vastplan.shared-state.schema.json
var schemaJSON []byte

var (
	schemaOnce sync.Once
	schemas    map[string]*jsonschema.Schema
	schemaErr  error
)

func compileSchemas() {
	compiler := jsonschema.NewCompiler()
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemaJSON))
	if err != nil {
		schemaErr = err
		return
	}
	const resource = "https://schemas.cdsoft.com.cn/vastplan/shared-state/v1/vastplan.shared-state.schema.json"
	if err := compiler.AddResource(resource, document); err != nil {
		schemaErr = err
		return
	}
	schemas = map[string]*jsonschema.Schema{}
	for _, name := range []string{"keyRequest", "createRequest", "updateRequest", "deleteRequest", "listRequest", "entry", "page", "ack"} {
		schemas[name], err = compiler.Compile(resource + "#/$defs/" + name)
		if err != nil {
			schemaErr = err
			return
		}
	}
}

func ParseRequest(operation string, raw []byte) (any, error) {
	definition := ""
	var target any
	switch operation {
	case OperationGet:
		definition, target = "keyRequest", &KeyRequest{}
	case OperationCreate:
		definition, target = "createRequest", &WriteRequest{}
	case OperationUpdate:
		definition, target = "updateRequest", &WriteRequest{}
	case OperationDelete:
		definition, target = "deleteRequest", &DeleteRequest{}
	case OperationList:
		definition, target = "listRequest", &ListRequest{}
	default:
		return nil, fmt.Errorf("不支持的 Shared State 操作 %q", operation)
	}
	if err := parse(definition, raw, target); err != nil {
		return nil, err
	}
	if request, ok := target.(*WriteRequest); ok {
		value, err := DecodeValue(request.Value)
		if err != nil || len(value) > 1<<20 {
			return nil, errors.New("Shared State value 无效")
		}
	}
	return target, nil
}

func ParseEntry(raw []byte) (Entry, error) {
	var value Entry
	if err := parse("entry", raw, &value); err != nil || value.Protocol != Protocol || value.UpdatedAt.IsZero() {
		return Entry{}, errors.New("Shared State entry 无效")
	}
	if _, err := DecodeValue(value.Value); err != nil {
		return Entry{}, errors.New("Shared State entry value 无效")
	}
	return value, nil
}

func ParsePage(raw []byte) (Page, error) {
	var value Page
	if err := parse("page", raw, &value); err != nil || value.Protocol != Protocol {
		return Page{}, errors.New("Shared State page 无效")
	}
	for _, item := range value.Items {
		encoded, _ := json.Marshal(item)
		if _, err := ParseEntry(encoded); err != nil {
			return Page{}, errors.New("Shared State page item 无效")
		}
	}
	return value, nil
}

func ParseAck(raw []byte) error {
	var value Ack
	if err := parse("ack", raw, &value); err != nil || value.Protocol != Protocol {
		return errors.New("Shared State ack 无效")
	}
	return nil
}

func EncodeValue(value []byte) string { return base64.RawURLEncoding.EncodeToString(value) }

func DecodeValue(value string) ([]byte, error) {
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || base64.RawURLEncoding.EncodeToString(raw) != value {
		return nil, errors.New("value 必须是规范 base64url")
	}
	return raw, nil
}

func parse(definition string, raw []byte, target any) error {
	schemaOnce.Do(compileSchemas)
	if schemaErr != nil {
		return schemaErr
	}
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return err
	}
	if err := schemas[definition].Validate(instance); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("Shared State 只能包含一个 JSON 文档")
	}
	return nil
}
