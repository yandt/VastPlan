package versioningv1

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// rejectDuplicateJSONKeys prevents different decoders from interpreting the
// same signed or hashed JSON payload differently.
func rejectDuplicateJSONKeys(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := scanUniqueJSONValue(decoder, "$", 0); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("JSON 只能包含一个文档")
		}
		return fmt.Errorf("读取 JSON 结尾: %w", err)
	}
	return nil
}

func scanUniqueJSONValue(decoder *json.Decoder, path string, depth int) error {
	if depth > maxContentDepth {
		return errors.New("JSON 深度超过限制")
	}
	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("解析 JSON: %w", err)
	}
	delimiter, composite := token.(json.Delim)
	if !composite {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]struct{}{}
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return fmt.Errorf("解析 JSON object key: %w", err)
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("JSON object key 必须是字符串")
			}
			if _, duplicate := seen[key]; duplicate {
				return fmt.Errorf("JSON 路径 %s 存在重复 key %q", path, key)
			}
			seen[key] = struct{}{}
			if err := scanUniqueJSONValue(decoder, path+"."+key, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for index := 0; decoder.More(); index++ {
			if err := scanUniqueJSONValue(decoder, fmt.Sprintf("%s[%d]", path, index), depth+1); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("JSON 出现无效 delimiter %q", delimiter)
	}
	closing, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("解析 JSON 结束符: %w", err)
	}
	expected := json.Delim('}')
	if delimiter == '[' {
		expected = ']'
	}
	if closing != expected {
		return fmt.Errorf("JSON 结束符无效: 期待 %q，实际 %q", expected, closing)
	}
	return nil
}
