// Package commonv1 登记多个 VastPlan JSON Schema 版本共用的稳定基础定义。
//
// 插件清单、制品索引和期望态引用同一份标识规则；部署 v1/v2 共用同一份资源 DTO，
// 避免正则或完全同义的 Go 结构在版本包之间漂移。
package commonv1

import (
	"bytes"
	_ "embed"
	"fmt"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// IdentifiersSchemaURL 是公共标识符 Schema 的稳定标识。
const IdentifiersSchemaURL = "https://schemas.cdsoft.com.cn/vastplan/common/v1/identifiers.schema.json"

//go:embed identifiers.schema.json
var identifiersSchemaJSON []byte

// AddResources 把公共 Schema 登记到调用方的编译器。每个领域包仍自行编译自己的根 Schema，
// 这里只提供跨领域引用所需的唯一资源。
func AddResources(compiler *jsonschema.Compiler) error {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(identifiersSchemaJSON))
	if err != nil {
		return fmt.Errorf("解析公共标识符 Schema: %w", err)
	}
	if err := compiler.AddResource(IdentifiersSchemaURL, doc); err != nil {
		return fmt.Errorf("登记公共标识符 Schema: %w", err)
	}
	return nil
}
