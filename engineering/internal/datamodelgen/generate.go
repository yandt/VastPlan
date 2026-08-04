package datamodelgen

import (
	"fmt"
	"go/token"
	"sort"

	datamodelv1 "cdsoft.com.cn/VastPlan/contracts/schemas/datamodel/v1"
)

type Language string

const (
	Go         Language = "go"
	TypeScript Language = "typescript"
	Python     Language = "python"
)

type Output struct {
	Filename string
	Content  []byte
}

func Generate(model datamodelv1.Model, language Language, packageName string) (Output, error) {
	if err := datamodelv1.Validate(model); err != nil {
		return Output{}, err
	}
	if packageName == "" || !token.IsIdentifier(packageName) || token.Lookup(packageName).IsKeyword() {
		return Output{}, fmt.Errorf("生成包名必须是有效标识符")
	}
	switch language {
	case Go:
		return generateGo(model, packageName)
	case TypeScript:
		return Output{Filename: fileStem(model.ID) + ".generated.ts", Content: generateTypeScript(model)}, nil
	case Python:
		return Output{Filename: fileStem(model.ID) + "_generated.py", Content: generatePython(model)}, nil
	default:
		return Output{}, fmt.Errorf("不支持的生成语言 %q", language)
	}
}

func SupportedLanguages() []Language {
	values := []Language{Go, TypeScript, Python}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	return values
}
