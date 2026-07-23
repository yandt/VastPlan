package configurationscopedv1

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

const maxValuesBytes = 64 << 10

//go:embed vastplan.configuration-scoped.schema.json
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
		schemaErr = fmt.Errorf("解析 Scoped Configuration Schema: %w", err)
		return
	}
	if err := compiler.AddResource(SchemaURL, document); err != nil {
		schemaErr = fmt.Errorf("登记 Scoped Configuration Schema: %w", err)
		return
	}
	schemas = map[string]*jsonschema.Schema{}
	for _, definition := range []string{"resolveRequest", "watchRevisionRequest", "resolution", "revisionObservation"} {
		compiled, compileErr := compiler.Compile(SchemaURL + "#/$defs/" + definition)
		if compileErr != nil {
			schemaErr = fmt.Errorf("编译 Scoped Configuration Schema %s: %w", definition, compileErr)
			return
		}
		schemas[definition] = compiled
	}
}

func validateDefinition(definition string, raw []byte) error {
	schemaOnce.Do(compileSchemas)
	if schemaErr != nil {
		return schemaErr
	}
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("解析 Scoped Configuration JSON: %w", err)
	}
	if err := schemas[definition].Validate(instance); err != nil {
		return fmt.Errorf("Scoped Configuration %s 不符合 Schema: %w", definition, err)
	}
	return nil
}

func ParseRequest(operation string, raw []byte) (any, error) {
	var definition string
	var target any
	switch operation {
	case OperationResolve:
		definition, target = "resolveRequest", &ResolveRequest{}
	case OperationWatchRevision:
		definition, target = "watchRevisionRequest", &WatchRevisionRequest{}
	default:
		return nil, fmt.Errorf("不支持的 Scoped Configuration 操作 %q", operation)
	}
	if err := validateDefinition(definition, raw); err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, errors.New("Scoped Configuration 只能包含一个 JSON 文档")
	}
	return target, nil
}

func DigestValues(values json.RawMessage) (string, error) {
	if len(values) == 0 || len(values) > maxValuesBytes {
		return "", errors.New("Scoped Configuration values 大小无效")
	}
	var object map[string]any
	if err := json.Unmarshal(values, &object); err != nil || object == nil {
		return "", errors.New("Scoped Configuration values 必须是 JSON 对象")
	}
	normalized, err := json.Marshal(object)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(normalized)
	return hex.EncodeToString(digest[:]), nil
}

func ValidateResolution(response Resolution) error {
	raw, err := json.Marshal(response)
	if err != nil {
		return err
	}
	if err := validateDefinition("resolution", raw); err != nil {
		return err
	}
	if response.Protocol != Protocol || response.ObservedAt.IsZero() || (response.Source != "seed" && response.Source != "active") {
		return errors.New("Scoped Configuration resolution 身份无效")
	}
	digest, err := DigestValues(response.Values)
	if err != nil || digest != response.Digest {
		return errors.New("Scoped Configuration resolution 摘要无效")
	}
	if (response.Revision == 0) != (response.Source == "seed") {
		return errors.New("Scoped Configuration seed/active revision 不一致")
	}
	return nil
}

func ValidateRevisionObservation(response RevisionObservation) error {
	raw, err := json.Marshal(response)
	if err != nil {
		return err
	}
	if err := validateDefinition("revisionObservation", raw); err != nil {
		return err
	}
	if response.Protocol != Protocol || response.ObservedAt.IsZero() {
		return errors.New("Scoped Configuration revision observation 身份无效")
	}
	return nil
}
