package datamodelgen

import (
	"strings"
	"unicode"
)

func fileStem(id string) string {
	parts := strings.FieldsFunc(id, func(r rune) bool { return r == '.' || r == '-' })
	return strings.Join(parts, "_")
}

func exportedName(id string) string {
	parts := strings.FieldsFunc(id, func(r rune) bool { return r == '.' || r == '-' || r == '_' })
	for i := range parts {
		if parts[i] == "" {
			continue
		}
		runes := []rune(parts[i])
		runes[0] = unicode.ToUpper(runes[0])
		parts[i] = string(runes)
	}
	value := strings.Join(parts, "")
	if value == "Id" {
		return "ID"
	}
	if strings.HasSuffix(value, "Id") {
		return strings.TrimSuffix(value, "Id") + "ID"
	}
	return value
}

func pythonName(id string) string {
	var values []rune
	for i, r := range id {
		if unicode.IsUpper(r) && i > 0 {
			values = append(values, '_')
		}
		if r == '.' || r == '-' {
			r = '_'
		}
		values = append(values, unicode.ToLower(r))
	}
	return string(values)
}
