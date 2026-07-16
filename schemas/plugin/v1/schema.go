// Package pluginv1 提供 VastPlan 插件 JSON Schema 的运行时校验入口。
//
// JSON Schema 文件与本包同目录，使 Go 可将它们编译进二进制；文件本身仍是清单、
// 制品元数据和运行时 descriptor 的唯一契约源。其他语言实现必须消费同一批 .json，
// 不得把规则复制成另一套手写类型。
package pluginv1

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"sync"

	commonv1 "cdsoft.com.cn/VastPlan/schemas/common/v1"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

const (
	// ManifestSchemaURL 是插件清单 Schema 的稳定标识。
	ManifestSchemaURL = "https://schemas.cdsoft.com.cn/vastplan/plugin/v1/vastplan.plugin.schema.json"
	// DescriptorSchemaURL 是运行态 contribution descriptor Schema 的稳定标识。
	DescriptorSchemaURL = "https://schemas.cdsoft.com.cn/vastplan/plugin/v1/vastplan.descriptor.schema.json"
	// ArtifactSchemaURL 是制品仓库元数据 Schema 的稳定标识。
	ArtifactSchemaURL = "https://schemas.cdsoft.com.cn/vastplan/plugin/v1/vastplan.artifact.schema.json"
)

//go:embed vastplan.plugin.schema.json
var manifestSchemaJSON []byte

//go:embed vastplan.descriptor.schema.json
var descriptorSchemaJSON []byte

//go:embed vastplan.artifact.schema.json
var artifactSchemaJSON []byte

var (
	compileOnce   sync.Once
	manifestSch   *jsonschema.Schema
	descriptorSch *jsonschema.Schema
	artifactSch   *jsonschema.Schema
	compileErr    error
)

// Manifest 是清单中制品服务需要读取的稳定字段。Contributes 保留原始 JSON，
// 因为每个扩展点的详细 descriptor 由 Schema 而非一套会漂移的 Go struct 描述。
type Manifest struct {
	ID           string                     `json:"id"`
	Name         string                     `json:"name"`
	Description  string                     `json:"description"`
	Version      string                     `json:"version"`
	Publisher    string                     `json:"publisher"`
	Engines      map[string]string          `json:"engines"`
	Capabilities *Capabilities              `json:"capabilities,omitempty"`
	Activation   []string                   `json:"activation"`
	Dependencies map[string]string          `json:"dependencies,omitempty"`
	Entry        map[string]string          `json:"entry"`
	Contributes  map[string]json.RawMessage `json:"contributes"`
}

// Capabilities 是装配元数据，不承担运行时权限强制职责。
type Capabilities struct {
	KernelServices []string `json:"kernelServices,omitempty"`
	Credentials    []string `json:"credentials,omitempty"`
	Resources      []string `json:"resources,omitempty"`
}

func schemas() error {
	compileOnce.Do(func() {
		compiler := jsonschema.NewCompiler()
		if err := commonv1.AddResources(compiler); err != nil {
			compileErr = err
			return
		}
		for url, raw := range map[string][]byte{
			ManifestSchemaURL:   manifestSchemaJSON,
			DescriptorSchemaURL: descriptorSchemaJSON,
			ArtifactSchemaURL:   artifactSchemaJSON,
		} {
			doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
			if err != nil {
				compileErr = fmt.Errorf("解析内置 Schema %s: %w", url, err)
				return
			}
			if err := compiler.AddResource(url, doc); err != nil {
				compileErr = fmt.Errorf("登记内置 Schema %s: %w", url, err)
				return
			}
		}
		manifestSch, compileErr = compiler.Compile(ManifestSchemaURL)
		if compileErr != nil {
			compileErr = fmt.Errorf("编译插件清单 Schema: %w", compileErr)
			return
		}
		descriptorSch, compileErr = compiler.Compile(DescriptorSchemaURL)
		if compileErr != nil {
			compileErr = fmt.Errorf("编译 descriptor Schema: %w", compileErr)
			return
		}
		artifactSch, compileErr = compiler.Compile(ArtifactSchemaURL)
		if compileErr != nil {
			compileErr = fmt.Errorf("编译制品元数据 Schema: %w", compileErr)
		}
	})
	return compileErr
}

// ParseManifest 校验并解析清单。任何未知字段、缺失必填字段或不合法 descriptor
// 都在制品进入仓库前被拒绝，调用方不可绕过 Schema 直接反序列化。
func ParseManifest(raw []byte) (Manifest, error) {
	if err := schemas(); err != nil {
		return Manifest{}, err
	}
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return Manifest{}, fmt.Errorf("解析插件清单 JSON: %w", err)
	}
	if err := manifestSch.Validate(instance); err != nil {
		return Manifest{}, fmt.Errorf("插件清单不符合 Schema: %w", err)
	}
	var manifest Manifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("解析插件清单字段: %w", err)
	}
	return manifest, nil
}

// ValidateDescriptor 校验插件通过协议总线注册的一条运行态 descriptor。
// 它把 extension point 和 descriptor 一起送入 Schema，避免只校验 JSON 语法而放过
// 诸如 hook phase 拼错这类会让分发语义失真的错误。
func ValidateDescriptor(extensionPoint string, raw []byte) error {
	if err := schemas(); err != nil {
		return err
	}
	var descriptor any
	if err := json.Unmarshal(raw, &descriptor); err != nil {
		return fmt.Errorf("解析 %s descriptor JSON: %w", extensionPoint, err)
	}
	instance := map[string]any{"extensionPoint": extensionPoint, "descriptor": descriptor}
	if err := descriptorSch.Validate(instance); err != nil {
		return fmt.Errorf("%s descriptor 不符合 Schema: %w", extensionPoint, err)
	}
	return nil
}

// ValidateArtifactMetadata 校验制品索引 JSON；仓库发布和读取都调用它，避免索引
// 被手工写坏后仍被下游 reconcile 采用。
func ValidateArtifactMetadata(raw []byte) error {
	if err := schemas(); err != nil {
		return err
	}
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("解析制品元数据 JSON: %w", err)
	}
	if err := artifactSch.Validate(instance); err != nil {
		return fmt.Errorf("制品元数据不符合 Schema: %w", err)
	}
	return nil
}
