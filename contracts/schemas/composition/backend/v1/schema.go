// Package backendcompositionv1 defines Backend's two authorized composition
// inputs. It reuses the cross-kernel document, target and digest contract while
// keeping service scheduling DTOs strictly Backend-specific.
package backendcompositionv1

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"

	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
	deploymentv1 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v1"
	deploymentv2 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v2"
	"cdsoft.com.cn/VastPlan/core/shared/go/configfile"
)

const (
	PlatformProfileSchemaURL        = "https://schemas.cdsoft.com.cn/vastplan/composition/backend/v1/vastplan.platform-profile.schema.json"
	ApplicationCompositionSchemaURL = "https://schemas.cdsoft.com.cn/vastplan/composition/backend/v1/vastplan.application-composition.schema.json"
	BackendPlatformCatalogSchemaURL = "https://schemas.cdsoft.com.cn/vastplan/composition/backend/v1/vastplan.backend-platform-catalog.schema.json"
)

//go:embed vastplan.platform-profile.schema.json
var platformProfileSchemaJSON []byte

//go:embed vastplan.application-composition.schema.json
var applicationCompositionSchemaJSON []byte

//go:embed vastplan.backend-platform-catalog.schema.json
var backendPlatformCatalogSchemaJSON []byte

var (
	compileOnce           sync.Once
	platformProfileSchema *jsonschema.Schema
	applicationSchema     *jsonschema.Schema
	backendCatalogSchema  *jsonschema.Schema
	compileErr            error
)

type PlatformProfile struct {
	compositioncommonv1.Document
	Target         compositioncommonv1.Target `json:"target"`
	ServiceClasses []string                   `json:"serviceClasses"`
	Attachments    []Attachment               `json:"attachments"`
	Services       []deploymentv2.ServiceUnit `json:"services"`
}

type Attachment struct {
	ServiceClass string                   `json:"serviceClass"`
	Plugins      []deploymentv1.PluginRef `json:"plugins"`
}

type ApplicationComposition struct {
	compositioncommonv1.Document
	Target   compositioncommonv1.Target `json:"target"`
	Metadata deploymentv1.Metadata      `json:"metadata"`
	Units    []ApplicationUnit          `json:"units"`
}

type ApplicationUnit struct {
	ServiceClass string                   `json:"serviceClass"`
	Spec         deploymentv2.ServiceUnit `json:"spec"`
}

// BackendPlatformCatalog is the platform-owned authority for selecting the
// immutable Platform Profile used by an online-managed deployment. Application
// administrators may edit only ApplicationComposition; they cannot add or
// replace bindings through the deployment-manager API.
type BackendPlatformCatalog struct {
	compositioncommonv1.Document
	Profiles []PlatformProfile        `json:"profiles"`
	Bindings []BackendPlatformBinding `json:"bindings"`
}

type BackendPlatformBinding struct {
	TenantID        string                  `json:"tenantId"`
	DeploymentName  string                  `json:"deploymentName"`
	PlatformProfile compositioncommonv1.Ref `json:"platformProfile"`
}

func schemas() (*jsonschema.Schema, *jsonschema.Schema, error) {
	compileOnce.Do(func() {
		compiler := jsonschema.NewCompiler()
		if err := deploymentv2.AddResources(compiler); err != nil {
			compileErr = err
			return
		}
		for _, resource := range []struct {
			url string
			raw []byte
		}{{PlatformProfileSchemaURL, platformProfileSchemaJSON}, {ApplicationCompositionSchemaURL, applicationCompositionSchemaJSON}, {BackendPlatformCatalogSchemaURL, backendPlatformCatalogSchemaJSON}} {
			document, err := jsonschema.UnmarshalJSON(bytes.NewReader(resource.raw))
			if err != nil {
				compileErr = fmt.Errorf("解析 Backend 组合 Schema %s: %w", resource.url, err)
				return
			}
			if err := compiler.AddResource(resource.url, document); err != nil {
				compileErr = fmt.Errorf("登记 Backend 组合 Schema %s: %w", resource.url, err)
				return
			}
		}
		platformProfileSchema, compileErr = compiler.Compile(PlatformProfileSchemaURL)
		if compileErr != nil {
			compileErr = fmt.Errorf("编译 Backend Platform Profile Schema: %w", compileErr)
			return
		}
		applicationSchema, compileErr = compiler.Compile(ApplicationCompositionSchemaURL)
		if compileErr != nil {
			compileErr = fmt.Errorf("编译 Backend Application Composition Schema: %w", compileErr)
			return
		}
		backendCatalogSchema, compileErr = compiler.Compile(BackendPlatformCatalogSchemaURL)
		if compileErr != nil {
			compileErr = fmt.Errorf("编译 Backend Platform Catalog Schema: %w", compileErr)
		}
	})
	return platformProfileSchema, applicationSchema, compileErr
}

func catalogSchema() (*jsonschema.Schema, error) {
	_, _, err := schemas()
	return backendCatalogSchema, err
}

func ParsePlatformProfile(raw []byte) (PlatformProfile, error) {
	platformSchema, _, err := schemas()
	if err != nil {
		return PlatformProfile{}, err
	}
	if err := validateJSON(platformSchema, raw, "Backend Platform Profile"); err != nil {
		return PlatformProfile{}, err
	}
	var profile PlatformProfile
	if err := json.Unmarshal(raw, &profile); err != nil {
		return PlatformProfile{}, fmt.Errorf("解析 Backend Platform Profile 字段: %w", err)
	}
	if err := compositioncommonv1.ValidateTarget(profile.Target, compositioncommonv1.KernelBackend); err != nil {
		return PlatformProfile{}, err
	}
	classes := make(map[string]struct{}, len(profile.ServiceClasses))
	for _, serviceClass := range profile.ServiceClasses {
		classes[serviceClass] = struct{}{}
	}
	for i := range profile.Attachments {
		attachment := &profile.Attachments[i]
		if _, ok := classes[attachment.ServiceClass]; !ok {
			return PlatformProfile{}, fmt.Errorf("Backend Platform Profile attachment 使用未声明 serviceClass %q", attachment.ServiceClass)
		}
		seen := map[string]struct{}{}
		for j := range attachment.Plugins {
			plugin := &attachment.Plugins[j]
			if plugin.Channel == "" {
				plugin.Channel = "stable"
			}
			if _, duplicate := seen[plugin.ID]; duplicate {
				return PlatformProfile{}, fmt.Errorf("Backend Platform Profile serviceClass %q 的插件 id 重复: %q", attachment.ServiceClass, plugin.ID)
			}
			seen[plugin.ID] = struct{}{}
		}
	}
	profile.Services, err = deploymentv2.NormalizeServiceUnits(profile.Services)
	if err != nil {
		return PlatformProfile{}, fmt.Errorf("Backend Platform Profile services 无效: %w", err)
	}
	return profile, nil
}

func ParseApplicationComposition(raw []byte) (ApplicationComposition, error) {
	_, applicationSchema, err := schemas()
	if err != nil {
		return ApplicationComposition{}, err
	}
	if err := validateJSON(applicationSchema, raw, "Backend Application Composition"); err != nil {
		return ApplicationComposition{}, err
	}
	var composition ApplicationComposition
	if err := json.Unmarshal(raw, &composition); err != nil {
		return ApplicationComposition{}, fmt.Errorf("解析 Backend Application Composition 字段: %w", err)
	}
	if err := compositioncommonv1.ValidateTarget(composition.Target, compositioncommonv1.KernelBackend); err != nil {
		return ApplicationComposition{}, err
	}
	units := make([]deploymentv2.ServiceUnit, len(composition.Units))
	for i := range composition.Units {
		units[i] = composition.Units[i].Spec
	}
	units, err = deploymentv2.NormalizeServiceUnits(units)
	if err != nil {
		return ApplicationComposition{}, fmt.Errorf("Backend Application Composition units 无效: %w", err)
	}
	for i := range composition.Units {
		composition.Units[i].Spec = units[i]
	}
	return composition, nil
}

func ValidatePlatformProfile(profile PlatformProfile) (PlatformProfile, error) {
	raw, err := json.Marshal(profile)
	if err != nil {
		return PlatformProfile{}, fmt.Errorf("编码 Backend Platform Profile: %w", err)
	}
	return ParsePlatformProfile(raw)
}

func ValidateApplicationComposition(composition ApplicationComposition) (ApplicationComposition, error) {
	raw, err := json.Marshal(composition)
	if err != nil {
		return ApplicationComposition{}, fmt.Errorf("编码 Backend Application Composition: %w", err)
	}
	return ParseApplicationComposition(raw)
}

func ParseBackendPlatformCatalog(raw []byte) (BackendPlatformCatalog, error) {
	schema, err := catalogSchema()
	if err != nil {
		return BackendPlatformCatalog{}, err
	}
	if err := validateJSON(schema, raw, "Backend Platform Catalog"); err != nil {
		return BackendPlatformCatalog{}, err
	}
	var catalog BackendPlatformCatalog
	if err := json.Unmarshal(raw, &catalog); err != nil {
		return BackendPlatformCatalog{}, fmt.Errorf("解析 Backend Platform Catalog 字段: %w", err)
	}
	profiles := make(map[string]PlatformProfile, len(catalog.Profiles))
	for i := range catalog.Profiles {
		profile, err := ValidatePlatformProfile(catalog.Profiles[i])
		if err != nil {
			return BackendPlatformCatalog{}, fmt.Errorf("Backend Platform Catalog profile[%d]: %w", i, err)
		}
		key := profileRefKey(profile.ID, profile.Revision, profile.Digest())
		if _, duplicate := profiles[key]; duplicate {
			return BackendPlatformCatalog{}, fmt.Errorf("Backend Platform Catalog profile 重复: %s", profile.ID)
		}
		profiles[key] = profile
		catalog.Profiles[i] = profile
	}
	bindings := map[string]struct{}{}
	for _, binding := range catalog.Bindings {
		key := binding.TenantID + "\x00" + binding.DeploymentName
		if _, duplicate := bindings[key]; duplicate {
			return BackendPlatformCatalog{}, fmt.Errorf("Backend Platform Catalog binding 重复: tenant=%q deployment=%q", binding.TenantID, binding.DeploymentName)
		}
		bindings[key] = struct{}{}
		refKey := profileRefKey(binding.PlatformProfile.ID, binding.PlatformProfile.Revision, binding.PlatformProfile.Digest)
		if _, ok := profiles[refKey]; !ok {
			return BackendPlatformCatalog{}, fmt.Errorf("Backend Platform Catalog binding 引用未登记的精确 Platform Profile: tenant=%q deployment=%q", binding.TenantID, binding.DeploymentName)
		}
	}
	return catalog, nil
}

func ValidateBackendPlatformCatalog(catalog BackendPlatformCatalog) (BackendPlatformCatalog, error) {
	raw, err := json.Marshal(catalog)
	if err != nil {
		return BackendPlatformCatalog{}, fmt.Errorf("编码 Backend Platform Catalog: %w", err)
	}
	return ParseBackendPlatformCatalog(raw)
}

func ParseBackendPlatformCatalogFile(filename string) (BackendPlatformCatalog, error) {
	raw, err := configfile.Load(filename)
	if err != nil {
		return BackendPlatformCatalog{}, fmt.Errorf("读取 Backend Platform Catalog 文件: %w", err)
	}
	return ParseBackendPlatformCatalog(raw)
}

func (c BackendPlatformCatalog) Resolve(tenantID, deploymentName string) (PlatformProfile, compositioncommonv1.Ref, error) {
	for _, binding := range c.Bindings {
		if binding.TenantID != tenantID || binding.DeploymentName != deploymentName {
			continue
		}
		for _, profile := range c.Profiles {
			if profile.ID == binding.PlatformProfile.ID && profile.Revision == binding.PlatformProfile.Revision && profile.Digest() == binding.PlatformProfile.Digest {
				return profile, binding.PlatformProfile, nil
			}
		}
		return PlatformProfile{}, compositioncommonv1.Ref{}, fmt.Errorf("Backend Platform Catalog binding 的 Platform Profile 不可解析")
	}
	return PlatformProfile{}, compositioncommonv1.Ref{}, fmt.Errorf("Backend Platform Catalog 未授权 tenant=%q deployment=%q", tenantID, deploymentName)
}

func (c BackendPlatformCatalog) Targets(tenantID string) []BackendPlatformBinding {
	out := make([]BackendPlatformBinding, 0)
	for _, binding := range c.Bindings {
		if binding.TenantID == tenantID {
			out = append(out, binding)
		}
	}
	return out
}

func profileRefKey(id string, revision uint64, digest string) string {
	return fmt.Sprintf("%s\x00%d\x00%s", id, revision, digest)
}

func validateJSON(schema *jsonschema.Schema, raw []byte, noun string) error {
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("解析 %s JSON: %w", noun, err)
	}
	if err := schema.Validate(instance); err != nil {
		return fmt.Errorf("%s 不符合 Schema: %w", noun, err)
	}
	return nil
}

func ParsePlatformProfileFile(filename string) (PlatformProfile, error) {
	raw, err := configfile.Load(filename)
	if err != nil {
		return PlatformProfile{}, fmt.Errorf("读取 Backend Platform Profile 文件: %w", err)
	}
	return ParsePlatformProfile(raw)
}

func ParseApplicationCompositionFile(filename string) (ApplicationComposition, error) {
	raw, err := configfile.Load(filename)
	if err != nil {
		return ApplicationComposition{}, fmt.Errorf("读取 Backend Application Composition 文件: %w", err)
	}
	return ParseApplicationComposition(raw)
}

func (p PlatformProfile) Digest() string { return compositioncommonv1.Digest(p) }

func (c ApplicationComposition) Digest() string { return compositioncommonv1.Digest(c) }
func (c BackendPlatformCatalog) Digest() string { return compositioncommonv1.Digest(c) }
