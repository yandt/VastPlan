package databasev1

import (
	"bytes"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/santhosh-tekuri/jsonschema/v6"

	commonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/common/v1"
)

//go:embed vastplan.database-runtime.schema.json
var schemaJSON []byte

var (
	schemaOnce        sync.Once
	definitionSchemas map[string]*jsonschema.Schema
	schemaErr         error
	decimalPattern    = regexp.MustCompile(`^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?$`)
)

var requestDefinition = map[string]string{
	OperationProviders: "providerListRequest",
	OperationProbe:     "probeRequest",
	OperationActivate:  "activateRequest",
	OperationRetire:    "retireRequest",
	OperationQuery:     "queryRequest",
	OperationExecute:   "executeRequest",
	OperationBegin:     "beginRequest",
	OperationCommit:    "endTransactionRequest",
	OperationRollback:  "endTransactionRequest",
}

var errorCodes = map[string]struct{}{
	ErrorInvalidRequest: {}, ErrorProviderNotFound: {}, ErrorUnsupported: {},
	ErrorConnectionNotFound: {}, ErrorConnectionUnavailable: {}, ErrorPoolExhausted: {},
	ErrorDeadlineExceeded: {}, ErrorQueryFailed: {}, ErrorTransactionLost: {},
	ErrorTransactionExpired: {}, ErrorTransactionConflict: {},
}

func compileSchemas() {
	compiler := jsonschema.NewCompiler()
	if err := commonv1.AddResources(compiler); err != nil {
		schemaErr = err
		return
	}
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemaJSON))
	if err != nil {
		schemaErr = fmt.Errorf("解析 Database Runtime Schema: %w", err)
		return
	}
	if err := compiler.AddResource(SchemaURL, document); err != nil {
		schemaErr = fmt.Errorf("登记 Database Runtime Schema: %w", err)
		return
	}
	definitions := []string{
		"providerDescriptor", "connectionRef", "connectionSpec", "statement", "queryResult",
		"providerListRequest", "probeRequest", "activateRequest", "retireRequest",
		"queryRequest", "executeRequest", "beginRequest", "endTransactionRequest",
	}
	definitionSchemas = make(map[string]*jsonschema.Schema, len(definitions))
	for _, definition := range definitions {
		compiled, compileErr := compiler.Compile(SchemaURL + "#/$defs/" + definition)
		if compileErr != nil {
			schemaErr = fmt.Errorf("编译 Database Runtime Schema %s: %w", definition, compileErr)
			return
		}
		definitionSchemas[definition] = compiled
	}
}

func validateDefinition(definition string, raw []byte) error {
	schemaOnce.Do(compileSchemas)
	if schemaErr != nil {
		return schemaErr
	}
	schema := definitionSchemas[definition]
	if schema == nil {
		return fmt.Errorf("未知 Database Runtime Schema 定义 %q", definition)
	}
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("解析 Database Runtime JSON: %w", err)
	}
	if err := schema.Validate(instance); err != nil {
		return fmt.Errorf("Database Runtime %s 不符合 Schema: %w", definition, err)
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
		return errors.New("Database Runtime 请求只能包含一个 JSON 文档")
	}
	return nil
}

// ParseRequest validates the operation-specific JSON Schema, rejects unknown
// fields and applies semantic rules that JSON Schema cannot safely express.
func ParseRequest(operation string, raw []byte) (any, error) {
	definition, ok := requestDefinition[operation]
	if !ok {
		return nil, fmt.Errorf("不支持的 Database Runtime 操作 %q", operation)
	}
	if err := validateDefinition(definition, raw); err != nil {
		return nil, err
	}
	var target any
	switch operation {
	case OperationProviders:
		target = &ProviderListRequest{}
	case OperationProbe:
		target = &ProbeRequest{}
	case OperationActivate:
		target = &ActivateRequest{}
	case OperationRetire:
		target = &RetireRequest{}
	case OperationQuery:
		target = &QueryRequest{}
	case OperationExecute:
		target = &ExecuteRequest{}
	case OperationBegin:
		target = &BeginRequest{}
	case OperationCommit, OperationRollback:
		target = &EndTransactionRequest{}
	}
	if err := decodeStrict(raw, target); err != nil {
		return nil, err
	}
	switch request := target.(type) {
	case *ProbeRequest:
		return request, ValidateConnectionSpec(request.Connection)
	case *ActivateRequest:
		return request, ValidateConnectionSpec(request.Connection)
	case *QueryRequest:
		return request, ValidateStatement(request.Statement)
	case *ExecuteRequest:
		return request, ValidateStatement(request.Statement)
	}
	return target, nil
}

func ValidateProviderDescriptor(descriptor ProviderDescriptor) error {
	raw, err := json.Marshal(descriptor)
	if err != nil {
		return err
	}
	if err := validateDefinition("providerDescriptor", raw); err != nil {
		return err
	}
	if descriptor.ID == "psql" {
		return errors.New("Provider ID 应使用 postgresql；psql 是客户端程序名")
	}
	if len(descriptor.ConfigurationSchema) > 64<<10 {
		return errors.New("Provider configurationSchema 超过 64KiB")
	}
	var root map[string]any
	if err := json.Unmarshal(descriptor.ConfigurationSchema, &root); err != nil || root == nil {
		return errors.New("Provider configurationSchema 必须是 JSON Schema 对象")
	}
	if root["type"] != "object" {
		return errors.New("Provider configurationSchema 根类型必须是 object")
	}
	if ref := externalSchemaRef(root); ref != "" {
		return fmt.Errorf("Provider configurationSchema 不得引用外部 Schema %q", ref)
	}
	compiler := jsonschema.NewCompiler()
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(descriptor.ConfigurationSchema))
	if err != nil {
		return fmt.Errorf("解析 Provider configurationSchema: %w", err)
	}
	resource := "https://schemas.cdsoft.com.cn/vastplan/database/provider/" + descriptor.ID + "/" + descriptor.Version + ".schema.json"
	if err := compiler.AddResource(resource, document); err != nil {
		return fmt.Errorf("登记 Provider configurationSchema: %w", err)
	}
	if _, err := compiler.Compile(resource); err != nil {
		return fmt.Errorf("编译 Provider configurationSchema: %w", err)
	}
	return nil
}

func externalSchemaRef(value any) string {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if key == "$ref" {
				if ref, ok := child.(string); ok && !strings.HasPrefix(ref, "#") {
					return ref
				}
			}
			if ref := externalSchemaRef(child); ref != "" {
				return ref
			}
		}
	case []any:
		for _, child := range typed {
			if ref := externalSchemaRef(child); ref != "" {
				return ref
			}
		}
	}
	return ""
}

func ValidateConnectionSpec(spec ConnectionSpec) error {
	raw, err := json.Marshal(spec)
	if err != nil {
		return err
	}
	if err := validateDefinition("connectionSpec", raw); err != nil {
		return err
	}
	if spec.Pool.MinIdle > spec.Pool.MaxIdle || spec.Pool.MaxIdle > spec.Pool.MaxOpen {
		return errors.New("连接池必须满足 minIdle <= maxIdle <= maxOpen")
	}
	if strings.IndexFunc(spec.Endpoint, unicode.IsSpace) >= 0 || strings.ContainsAny(spec.Endpoint, "@?;#") || strings.Contains(spec.Endpoint, "://") {
		return errors.New("endpoint 只能是非敏感主机/端口或 socket 标识，不能使用 DSN/URL")
	}
	var options map[string]any
	if err := json.Unmarshal(spec.Options, &options); err != nil || options == nil {
		return errors.New("Provider options 必须是 JSON 对象")
	}
	if key := secretOptionKey(options); key != "" {
		return fmt.Errorf("Provider options 不得包含疑似秘密字段 %q；请使用 CredentialRef", key)
	}
	return nil
}

func ValidateConnectionRef(ref ConnectionRef) error {
	raw, err := json.Marshal(ref)
	if err != nil {
		return err
	}
	return validateDefinition("connectionRef", raw)
}

func secretOptionKey(value any) string {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			normalized := strings.NewReplacer("_", "", "-", "", ".", "").Replace(strings.ToLower(key))
			for _, forbidden := range []string{"password", "passwd", "secret", "token", "credential", "privatekey", "accesskey", "clientkey", "sslkey"} {
				if strings.Contains(normalized, forbidden) {
					return key
				}
			}
			if nested := secretOptionKey(child); nested != "" {
				return key + "." + nested
			}
		}
	case []any:
		for _, child := range typed {
			if nested := secretOptionKey(child); nested != "" {
				return nested
			}
		}
	}
	return ""
}

func ValidateStatement(statement Statement) error {
	raw, err := json.Marshal(statement)
	if err != nil {
		return err
	}
	if err := validateDefinition("statement", raw); err != nil {
		return err
	}
	for index, value := range statement.Parameters {
		if err := ValidateValue(value); err != nil {
			return fmt.Errorf("参数 %d: %w", index, err)
		}
	}
	return nil
}

func ValidateValue(value Value) error {
	if value.Type == "null" {
		if len(value.Value) != 0 && string(value.Value) != "null" {
			return errors.New("null 值不得携带非 null 内容")
		}
		return nil
	}
	if len(value.Value) == 0 || !json.Valid(value.Value) {
		return errors.New("非 null 值必须携带一个合法 JSON value")
	}
	switch value.Type {
	case "string":
		var parsed string
		return json.Unmarshal(value.Value, &parsed)
	case "int64":
		var parsed string
		if err := json.Unmarshal(value.Value, &parsed); err != nil {
			return errors.New("int64 必须用十进制字符串编码")
		}
		_, err := strconv.ParseInt(parsed, 10, 64)
		return err
	case "decimal":
		var parsed string
		if err := json.Unmarshal(value.Value, &parsed); err != nil || !decimalPattern.MatchString(parsed) {
			return errors.New("decimal 必须用无损十进制字符串编码")
		}
		return nil
	case "bool":
		var parsed bool
		return json.Unmarshal(value.Value, &parsed)
	case "bytes":
		var parsed string
		if err := json.Unmarshal(value.Value, &parsed); err != nil {
			return errors.New("bytes 必须用 base64 字符串编码")
		}
		_, err := base64.StdEncoding.DecodeString(parsed)
		return err
	case "timestamp":
		var parsed string
		if err := json.Unmarshal(value.Value, &parsed); err != nil {
			return errors.New("timestamp 必须用 RFC3339 字符串编码")
		}
		_, err := time.Parse(time.RFC3339Nano, parsed)
		return err
	case "json":
		return nil
	default:
		return fmt.Errorf("未知数据库值类型 %q", value.Type)
	}
}

func ValidateQueryResult(result QueryResult) error {
	raw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	if err := validateDefinition("queryResult", raw); err != nil {
		return err
	}
	for rowIndex, row := range result.Rows {
		if len(row) != len(result.Columns) {
			return fmt.Errorf("第 %d 行列数=%d，与 columns=%d 不一致", rowIndex, len(row), len(result.Columns))
		}
		for columnIndex, value := range row {
			if err := ValidateValue(value); err != nil {
				return fmt.Errorf("第 %d 行第 %d 列: %w", rowIndex, columnIndex, err)
			}
		}
	}
	return nil
}

func KnownErrorCode(code string) bool {
	_, ok := errorCodes[code]
	return ok
}
