package databaseruntime

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
)

func statementArguments(statement databasev1.Statement) ([]any, error) {
	if err := databasev1.ValidateStatement(statement); err != nil {
		return nil, NewRuntimeError(databasev1.ErrorInvalidRequest, false, err)
	}
	arguments := make([]any, len(statement.Parameters))
	for index, value := range statement.Parameters {
		argument, err := valueArgument(value)
		if err != nil {
			return nil, NewRuntimeError(databasev1.ErrorInvalidRequest, false,
				fmt.Errorf("参数 %d: %w", index, err))
		}
		arguments[index] = argument
	}
	return arguments, nil
}

func valueArgument(value databasev1.Value) (any, error) {
	if err := databasev1.ValidateValue(value); err != nil {
		return nil, err
	}
	switch value.Type {
	case "null":
		return nil, nil
	case "string", "decimal":
		var text string
		if err := json.Unmarshal(value.Value, &text); err != nil {
			return nil, err
		}
		return text, nil
	case "int64":
		var text string
		if err := json.Unmarshal(value.Value, &text); err != nil {
			return nil, err
		}
		return strconv.ParseInt(text, 10, 64)
	case "bool":
		var boolean bool
		if err := json.Unmarshal(value.Value, &boolean); err != nil {
			return nil, err
		}
		return boolean, nil
	case "bytes":
		var encoded string
		if err := json.Unmarshal(value.Value, &encoded); err != nil {
			return nil, err
		}
		return base64.StdEncoding.DecodeString(encoded)
	case "timestamp":
		var encoded string
		if err := json.Unmarshal(value.Value, &encoded); err != nil {
			return nil, err
		}
		return time.Parse(time.RFC3339Nano, encoded)
	case "json":
		return string(value.Value), nil
	default:
		return nil, fmt.Errorf("不支持的参数类型 %q", value.Type)
	}
}

func scannedValue(value any, databaseType string) (databasev1.Value, error) {
	switch typed := value.(type) {
	case nil:
		return databasev1.Value{Type: "null"}, nil
	case bool:
		return marshaledValue("bool", typed)
	case int64:
		return marshaledValue("int64", strconv.FormatInt(typed, 10))
	case int32:
		return marshaledValue("int64", strconv.FormatInt(int64(typed), 10))
	case int:
		return marshaledValue("int64", strconv.FormatInt(int64(typed), 10))
	case uint64:
		if typed <= math.MaxInt64 {
			return marshaledValue("int64", strconv.FormatUint(typed, 10))
		}
		return marshaledValue("decimal", strconv.FormatUint(typed, 10))
	case float64:
		return marshaledValue("decimal", strconv.FormatFloat(typed, 'g', -1, 64))
	case float32:
		return marshaledValue("decimal", strconv.FormatFloat(float64(typed), 'g', -1, 32))
	case time.Time:
		return marshaledValue("timestamp", typed.UTC().Format(time.RFC3339Nano))
	case string:
		return scannedText([]byte(typed), databaseType)
	case []byte:
		return scannedText(typed, databaseType)
	default:
		return databasev1.Value{}, fmt.Errorf("数据库返回了不支持的 Go 类型 %T", value)
	}
}

func scannedText(raw []byte, databaseType string) (databasev1.Value, error) {
	normalized := strings.ToLower(strings.TrimSpace(databaseType))
	if isBinaryDatabaseType(normalized) {
		return marshaledValue("bytes", base64.StdEncoding.EncodeToString(raw))
	}
	if isJSONDatabaseType(normalized) && json.Valid(raw) {
		return databasev1.Value{Type: "json", Value: append(json.RawMessage(nil), raw...)}, nil
	}
	text := string(raw)
	if isIntegerDatabaseType(normalized) {
		if signed, err := strconv.ParseInt(text, 10, 64); err == nil {
			return marshaledValue("int64", strconv.FormatInt(signed, 10))
		}
		if unsigned, err := strconv.ParseUint(text, 10, 64); err == nil {
			if unsigned <= math.MaxInt64 {
				return marshaledValue("int64", strconv.FormatUint(unsigned, 10))
			}
			return marshaledValue("decimal", strconv.FormatUint(unsigned, 10))
		}
		return databasev1.Value{}, errors.New("数据库整数值格式无效")
	}
	if isDecimalDatabaseType(normalized) {
		value, err := marshaledValue("decimal", text)
		if err != nil {
			return databasev1.Value{}, err
		}
		if err := databasev1.ValidateValue(value); err != nil {
			return databasev1.Value{}, err
		}
		return value, nil
	}
	return marshaledValue("string", text)
}

func isIntegerDatabaseType(databaseType string) bool {
	for _, marker := range []string{"tinyint", "smallint", "mediumint", "bigint", "integer", "int2", "int4", "int8"} {
		if strings.Contains(databaseType, marker) {
			return true
		}
	}
	return false
}

func marshaledValue(valueType string, value any) (databasev1.Value, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return databasev1.Value{}, err
	}
	result := databasev1.Value{Type: valueType, Value: raw}
	if err := databasev1.ValidateValue(result); err != nil {
		return databasev1.Value{}, err
	}
	return result, nil
}

func isBinaryDatabaseType(databaseType string) bool {
	for _, marker := range []string{"bytea", "blob", "binary", "varbinary", "bit varying"} {
		if strings.Contains(databaseType, marker) {
			return true
		}
	}
	return false
}

func isJSONDatabaseType(databaseType string) bool {
	return databaseType == "json" || databaseType == "jsonb"
}

func isDecimalDatabaseType(databaseType string) bool {
	for _, marker := range []string{"decimal", "numeric", "money", "double", "float", "real"} {
		if strings.Contains(databaseType, marker) {
			return true
		}
	}
	return false
}

var errUnsupportedIsolation = errors.New("不支持的事务隔离级别")
