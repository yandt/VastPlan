package interaction

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	uiv1 "cdsoft.com.cn/VastPlan/contracts/schemas/ui/v1"
)

func validateResponse(request uiv1.InteractionRequest, response uiv1.InteractionResponse) error {
	if response.Decision == uiv1.DecisionRejected {
		if len(response.Values) != 0 || len(response.CredentialRef) != 0 {
			return fmt.Errorf("拒绝响应不能携带表单值")
		}
		return nil
	}
	secretKeys := map[string]bool{}
	collectSecretKeys(request.Form, secretKeys)
	if key := secretValuePath(response.Values, "", secretKeys); key != "" {
		return fmt.Errorf("秘密字段 %q 只能使用 credentialRefs", key)
	}
	for key, ref := range response.CredentialRef {
		if !secretKeys[normalizeCredentialPath(key)] || !validCredentialReference(ref) {
			return fmt.Errorf("credentialRefs 包含不允许的字段 %q", key)
		}
	}
	if request.Form != nil {
		values, err := mergeCredentialReferences(response.Values, response.CredentialRef)
		if err != nil {
			return err
		}
		if err := uiv1.ValidateFormData(*request.Form, values); err != nil {
			return fmt.Errorf("响应数据无效: %w", err)
		}
	}
	return nil
}

func collectSecretKeys(form *uiv1.FormSchema, keys map[string]bool) {
	if form == nil {
		return
	}
	root := map[string]any(form.Schema)
	visiting := map[string]bool{}
	var walk func(map[string]any, string)
	walk = func(schema map[string]any, parent string) {
		if ref, ok := schema["$ref"].(string); ok && strings.HasPrefix(ref, "#/") {
			visitKey := parent + "\x00" + ref
			if !visiting[visitKey] {
				visiting[visitKey] = true
				if resolved, ok := resolveLocalSchemaRef(root, ref); ok {
					walk(resolved, parent)
				}
				delete(visiting, visitKey)
			}
		}
		for _, keyword := range []string{"allOf", "anyOf", "oneOf"} {
			branches, _ := schema[keyword].([]any)
			for _, branch := range branches {
				if object, ok := branch.(map[string]any); ok {
					walk(object, parent)
				}
			}
		}
		for _, keyword := range []string{"if", "then", "else"} {
			if branch, ok := schema[keyword].(map[string]any); ok {
				walk(branch, parent)
			}
		}
		properties, _ := schema["properties"].(map[string]any)
		for name, raw := range properties {
			property, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			path := name
			if parent != "" {
				path = parent + "." + name
			}
			if property["format"] == "vastplan-credential-ref" && property["writeOnly"] == true {
				keys[path] = true
			}
			walk(property, path)
		}
		if items, ok := schema["items"].(map[string]any); ok {
			walk(items, parent+"[]")
		}
	}
	walk(root, "")
}

func resolveLocalSchemaRef(root map[string]any, ref string) (map[string]any, bool) {
	var current any = root
	for _, raw := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
		part := strings.ReplaceAll(strings.ReplaceAll(raw, "~1", "/"), "~0", "~")
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = object[part]
		if !ok {
			return nil, false
		}
	}
	resolved, ok := current.(map[string]any)
	return resolved, ok
}

func normalizeCredentialPath(path string) string {
	var result strings.Builder
	for index := 0; index < len(path); {
		if path[index] != '[' {
			result.WriteByte(path[index])
			index++
			continue
		}
		end := strings.IndexByte(path[index:], ']')
		if end < 0 {
			return path
		}
		end += index
		for _, digit := range path[index+1 : end] {
			if digit < '0' || digit > '9' {
				return path
			}
		}
		result.WriteString("[]")
		index = end + 1
	}
	return result.String()
}

func secretValuePath(value any, path string, secretKeys map[string]bool) string {
	if secretKeys[normalizeCredentialPath(path)] {
		return path
	}
	switch current := value.(type) {
	case map[string]any:
		for key, child := range current {
			childPath := key
			if path != "" {
				childPath = path + "." + key
			}
			if found := secretValuePath(child, childPath, secretKeys); found != "" {
				return found
			}
		}
	case []any:
		for index, child := range current {
			if found := secretValuePath(child, fmt.Sprintf("%s[%d]", path, index), secretKeys); found != "" {
				return found
			}
		}
	}
	return ""
}

func validCredentialReference(ref string) bool {
	if strings.TrimSpace(ref) != ref {
		return false
	}
	parsed, err := url.Parse(ref)
	return err == nil && parsed.Scheme == "credential" && parsed.Host != "" && parsed.User == nil
}

func mergeCredentialReferences(values map[string]any, references map[string]string) (map[string]any, error) {
	raw, err := json.Marshal(values)
	if err != nil {
		return nil, fmt.Errorf("复制响应值: %w", err)
	}
	merged := map[string]any{}
	if err := json.Unmarshal(raw, &merged); err != nil {
		return nil, fmt.Errorf("复制响应值: %w", err)
	}
	if merged == nil {
		merged = map[string]any{}
	}
	for path, ref := range references {
		parts, err := parseFormPath(path)
		if err != nil {
			return nil, fmt.Errorf("credentialRefs 字段路径 %q 无效: %w", path, err)
		}
		if err := setFormPath(merged, parts, ref); err != nil {
			return nil, fmt.Errorf("credentialRefs 字段路径 %q 无效: %w", path, err)
		}
	}
	return merged, nil
}

func parseFormPath(path string) ([]any, error) {
	if path == "" {
		return nil, fmt.Errorf("路径为空")
	}
	parts := []any{}
	for index := 0; index < len(path); {
		start := index
		for index < len(path) && path[index] != '.' && path[index] != '[' {
			index++
		}
		if start != index {
			parts = append(parts, path[start:index])
		}
		if index >= len(path) {
			break
		}
		if path[index] == '.' {
			index++
			continue
		}
		end := strings.IndexByte(path[index:], ']')
		if end < 0 {
			return nil, fmt.Errorf("缺少 ]")
		}
		end += index
		var arrayIndex int
		if _, err := fmt.Sscanf(path[index+1:end], "%d", &arrayIndex); err != nil || arrayIndex < 0 {
			return nil, fmt.Errorf("数组下标无效")
		}
		parts = append(parts, arrayIndex)
		index = end + 1
	}
	if len(parts) == 0 {
		return nil, fmt.Errorf("路径为空")
	}
	return parts, nil
}

func setFormPath(root map[string]any, parts []any, value string) error {
	var current any = root
	for index, part := range parts {
		last := index == len(parts)-1
		switch key := part.(type) {
		case string:
			object, ok := current.(map[string]any)
			if !ok {
				return fmt.Errorf("%q 的父节点不是对象", key)
			}
			if last {
				object[key] = value
				return nil
			}
			next, exists := object[key]
			if !exists {
				if _, ok := parts[index+1].(int); ok {
					return fmt.Errorf("数组父节点 %q 不存在", key)
				}
				next = map[string]any{}
				object[key] = next
			}
			current = next
		case int:
			array, ok := current.([]any)
			if !ok || key >= len(array) {
				return fmt.Errorf("数组下标 %d 越界", key)
			}
			if last {
				array[key] = value
				return nil
			}
			current = array[key]
		}
	}
	return fmt.Errorf("无法设置路径")
}
