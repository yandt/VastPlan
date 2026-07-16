// Package commonv1 登记多个 VastPlan JSON Schema 共用的标识符定义。
//
// 插件清单、制品索引和期望态都引用同一份 plugin ID、SemVer、channel 与相对路径规则，
// 避免复制正则后各自漂移。
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
