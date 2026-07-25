package pluginv1

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/santhosh-tekuri/jsonschema/v6"

	apiv1 "cdsoft.com.cn/VastPlan/contracts/schemas/api/v1"
	commonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/common/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/pluginid"
)

func schemas() error {
	compileOnce.Do(func() {
		compiler := jsonschema.NewCompiler()
		if err := commonv1.AddResources(compiler); err != nil {
			compileErr = err
			return
		}
		if err := apiv1.AddResources(compiler); err != nil {
			compileErr = err
			return
		}
		for url, raw := range map[string][]byte{
			ManifestSchemaURL:        manifestSchemaJSON,
			DescriptorSchemaURL:      descriptorSchemaJSON,
			ArtifactSchemaURL:        artifactSchemaJSON,
			ArtifactLockSchemaURL:    artifactLockSchemaJSON,
			ArtifactResolveSchemaURL: artifactResolveSchemaJSON,
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
			return
		}
		artifactLockSch, compileErr = compiler.Compile(ArtifactLockSchemaURL)
		if compileErr != nil {
			compileErr = fmt.Errorf("编译制品锁 Schema: %w", compileErr)
			return
		}
		artifactResolveSch, compileErr = compiler.Compile(ArtifactResolveSchemaURL)
		if compileErr != nil {
			compileErr = fmt.Errorf("编译制品解析输入 Schema: %w", compileErr)
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
	if err := pluginid.ValidatePublisherOwnership(manifest.ID, manifest.Publisher); err != nil {
		return Manifest{}, err
	}
	if manifest.Runtime != nil {
		if _, _, err := runtimePolicies(manifest); err != nil {
			return Manifest{}, err
		}
		if err := validateBackgroundService(manifest); err != nil {
			return Manifest{}, err
		}
	}
	if err := validateContextAccess(manifest.ContextAccess); err != nil {
		return Manifest{}, err
	}
	if err := validateConfiguration(manifest); err != nil {
		return Manifest{}, err
	}
	if err := validateCompositionFeatures(manifest); err != nil {
		return Manifest{}, err
	}
	if manifest.Configuration != nil && len(manifest.Configuration.ManagedCredentials) > 0 {
		found := false
		if manifest.Capabilities != nil {
			for _, capability := range manifest.Capabilities.KernelServices {
				if capability == "kernel.config.credential-ref" {
					found = true
					break
				}
			}
		}
		if !found {
			return Manifest{}, errors.New("configuration.managedCredentials 要求声明 kernel.config.credential-ref")
		}
	}
	if err := validateAuthorization(manifest); err != nil {
		return Manifest{}, err
	}
	if err := validateFrontendModuleGraphs(manifest); err != nil {
		return Manifest{}, err
	}
	if err := validateAuthenticationProviderDependencies(manifest); err != nil {
		return Manifest{}, err
	}
	if err := validateAPIContributions(manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}
