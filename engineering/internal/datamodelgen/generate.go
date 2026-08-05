package datamodelgen

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"go/token"
	"sort"

	commonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/common/v1"
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
	raw, err := json.Marshal(model)
	if err != nil {
		return Output{}, err
	}
	return GenerateWithSHA256(model, fmt.Sprintf("%x", sha256.Sum256(raw)), language, packageName)
}

func GenerateWithSHA256(model datamodelv1.Model, modelSHA256 string, language Language, packageName string) (Output, error) {
	if err := datamodelv1.Validate(model); err != nil {
		return Output{}, err
	}
	if !commonv1.IsSHA256(modelSHA256) {
		return Output{}, fmt.Errorf("DataModel SHA-256 无效")
	}
	if packageName == "" || !token.IsIdentifier(packageName) || token.Lookup(packageName).IsKeyword() {
		return Output{}, fmt.Errorf("生成包名必须是有效标识符")
	}
	switch language {
	case Go:
		return generateGo(model, modelSHA256, packageName)
	case TypeScript:
		return Output{Filename: fileStem(model.ID) + ".generated.ts", Content: generateTypeScript(model, modelSHA256)}, nil
	case Python:
		return Output{Filename: fileStem(model.ID) + "_generated.py", Content: generatePython(model, modelSHA256)}, nil
	default:
		return Output{}, fmt.Errorf("不支持的生成语言 %q", language)
	}
}

func SupportedLanguages() []Language {
	values := []Language{Go, TypeScript, Python}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	return values
}
