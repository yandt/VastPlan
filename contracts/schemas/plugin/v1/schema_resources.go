package pluginv1

import (
	"bytes"
	"fmt"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// AddArtifactSchemaResources 登记封闭的制品契约，但不重复登记其公共标识符依赖。
// 组合 Schema 必须先且只登记一次 common/v1，再调用本函数。
func AddArtifactSchemaResources(compiler *jsonschema.Compiler) error {
	if compiler == nil {
		return fmt.Errorf("artifact schema compiler 不能为空")
	}
	for _, resource := range []struct {
		url string
		raw []byte
	}{
		{ArtifactSchemaURL, artifactSchemaJSON},
		{ArtifactLockSchemaURL, artifactLockSchemaJSON},
		{ArtifactResolveSchemaURL, artifactResolveSchemaJSON},
	} {
		document, err := jsonschema.UnmarshalJSON(bytes.NewReader(resource.raw))
		if err != nil {
			return fmt.Errorf("解析 artifact Schema %s: %w", resource.url, err)
		}
		if err := compiler.AddResource(resource.url, document); err != nil {
			return fmt.Errorf("登记 artifact Schema %s: %w", resource.url, err)
		}
	}
	return nil
}
