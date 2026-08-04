package recordstore

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"
	"unicode/utf8"

	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
	datamodelv1 "cdsoft.com.cn/VastPlan/contracts/schemas/datamodel/v1"
)

func wireValue(field datamodelv1.Field, raw json.RawMessage) (databasev1.Value, error) {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		if !field.Nullable {
			return databasev1.Value{}, fmt.Errorf("字段 %s 不可为 null", field.ID)
		}
		return databasev1.Value{Type: "null"}, nil
	}
	if len(raw) == 0 || !json.Valid(raw) {
		return databasev1.Value{}, fmt.Errorf("字段 %s 不是合法 JSON value", field.ID)
	}
	value := databasev1.Value{Value: append(json.RawMessage(nil), raw...)}
	switch field.Type {
	case "string", "uuid":
		var parsed string
		if err := json.Unmarshal(raw, &parsed); err != nil || parsed == "" && field.Type == "uuid" {
			return databasev1.Value{}, fmt.Errorf("字段 %s 必须是字符串", field.ID)
		}
		if field.MaxLength > 0 && utf8.RuneCountInString(parsed) > field.MaxLength {
			return databasev1.Value{}, fmt.Errorf("字段 %s 超过 maxLength=%d", field.ID, field.MaxLength)
		}
		value.Type = "string"
	case "int64":
		var parsed string
		if err := json.Unmarshal(raw, &parsed); err != nil {
			return databasev1.Value{}, fmt.Errorf("字段 %s 的 int64 必须使用十进制字符串", field.ID)
		}
		if _, err := strconv.ParseInt(parsed, 10, 64); err != nil {
			return databasev1.Value{}, fmt.Errorf("字段 %s 的 int64 超出范围", field.ID)
		}
		value.Type = "int64"
	case "float64":
		var parsed json.Number
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		if err := decoder.Decode(&parsed); err != nil {
			return databasev1.Value{}, fmt.Errorf("字段 %s 必须是数值", field.ID)
		}
		value.Type, value.Value = "decimal", mustJSON(parsed.String())
	case "bool":
		var parsed bool
		if err := json.Unmarshal(raw, &parsed); err != nil {
			return databasev1.Value{}, fmt.Errorf("字段 %s 必须是布尔值", field.ID)
		}
		value.Type = "bool"
	case "bytes":
		var parsed string
		if err := json.Unmarshal(raw, &parsed); err != nil {
			return databasev1.Value{}, fmt.Errorf("字段 %s 必须是 base64 字符串", field.ID)
		}
		if _, err := base64.StdEncoding.DecodeString(parsed); err != nil {
			return databasev1.Value{}, fmt.Errorf("字段 %s 不是合法 base64", field.ID)
		}
		value.Type = "bytes"
	case "timestamp":
		var parsed string
		if err := json.Unmarshal(raw, &parsed); err != nil {
			return databasev1.Value{}, fmt.Errorf("字段 %s 必须是 RFC3339 时间", field.ID)
		}
		if _, err := time.Parse(time.RFC3339Nano, parsed); err != nil {
			return databasev1.Value{}, fmt.Errorf("字段 %s 必须是 RFC3339 时间", field.ID)
		}
		value.Type = "timestamp"
	case "json":
		value.Type = "json"
	default:
		return databasev1.Value{}, fmt.Errorf("字段 %s 类型不受支持", field.ID)
	}
	if err := databasev1.ValidateValue(value); err != nil {
		return databasev1.Value{}, err
	}
	return value, nil
}

func rawValue(field datamodelv1.Field, value databasev1.Value) (json.RawMessage, error) {
	if value.Type == "null" {
		if !field.Nullable {
			return nil, fmt.Errorf("数据库为非空字段 %s 返回 null", field.ID)
		}
		return json.RawMessage("null"), nil
	}
	if err := databasev1.ValidateValue(value); err != nil {
		return nil, err
	}
	expected := map[string]string{"string": "string", "uuid": "string", "int64": "int64", "float64": "decimal", "bool": "bool", "bytes": "bytes", "timestamp": "timestamp", "json": "json"}[field.Type]
	if value.Type != expected {
		return nil, errors.New("数据库值类型与 DataModel 不一致")
	}
	if field.Type == "float64" {
		var decimal string
		if err := json.Unmarshal(value.Value, &decimal); err != nil {
			return nil, err
		}
		return json.RawMessage(decimal), nil
	}
	return append(json.RawMessage(nil), value.Value...), nil
}

func mustJSON(value string) json.RawMessage {
	raw, _ := json.Marshal(value)
	return raw
}
