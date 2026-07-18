// Package compositionv1 defines the two authorized inputs used to assemble a
// backend service deployment. Platform Profile and Application Composition are
// deliberately separate resources with separate publishers and revisions.
package compositionv1

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"

	deploymentv1 "cdsoft.com.cn/VastPlan/schemas/deployment/v1"
	deploymentv2 "cdsoft.com.cn/VastPlan/schemas/deployment/v2"
)

const (
	PlatformProfileSchemaURL        = "https://schemas.cdsoft.com.cn/vastplan/composition/v1/vastplan.platform-profile.schema.json"
	ApplicationCompositionSchemaURL = "https://schemas.cdsoft.com.cn/vastplan/composition/v1/vastplan.application-composition.schema.json"
)

//go:embed vastplan.platform-profile.schema.json
var platformProfileSchemaJSON []byte

//go:embed vastplan.application-composition.schema.json
var applicationCompositionSchemaJSON []byte

var (
	compileOnce           sync.Once
	platformProfileSchema *jsonschema.Schema
	applicationSchema     *jsonschema.Schema
	compileErr            error
)

type Target struct {
	Kernel string `json:"kernel"`
}

type PlatformProfile struct {
	Version        int                        `json:"version"`
	Revision       uint64                     `json:"revision"`
	ID             string                     `json:"id"`
	Target         Target                     `json:"target"`
	ServiceClasses []string                   `json:"serviceClasses"`
	Attachments    []Attachment               `json:"attachments"`
	Services       []deploymentv2.ServiceUnit `json:"services"`
}

type Attachment struct {
	ServiceClass string                   `json:"serviceClass"`
	Plugins      []deploymentv1.PluginRef `json:"plugins"`
}

type ApplicationComposition struct {
	Version  int                   `json:"version"`
	Revision uint64                `json:"revision"`
	ID       string                `json:"id"`
	Kernel   string                `json:"kernel"`
	Metadata deploymentv1.Metadata `json:"metadata"`
	Units    []ApplicationUnit     `json:"units"`
}

type ApplicationUnit struct {
	ServiceClass string                   `json:"serviceClass"`
	Spec         deploymentv2.ServiceUnit `json:"spec"`
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
		}{{PlatformProfileSchemaURL, platformProfileSchemaJSON}, {ApplicationCompositionSchemaURL, applicationCompositionSchemaJSON}} {
			document, err := jsonschema.UnmarshalJSON(bytes.NewReader(resource.raw))
			if err != nil {
				compileErr = fmt.Errorf("解析组合 Schema %s: %w", resource.url, err)
				return
			}
			if err := compiler.AddResource(resource.url, document); err != nil {
				compileErr = fmt.Errorf("登记组合 Schema %s: %w", resource.url, err)
				return
			}
		}
		platformProfileSchema, compileErr = compiler.Compile(PlatformProfileSchemaURL)
		if compileErr != nil {
			compileErr = fmt.Errorf("编译 Platform Profile Schema: %w", compileErr)
			return
		}
		applicationSchema, compileErr = compiler.Compile(ApplicationCompositionSchemaURL)
		if compileErr != nil {
			compileErr = fmt.Errorf("编译 Application Composition Schema: %w", compileErr)
		}
	})
	return platformProfileSchema, applicationSchema, compileErr
}

func ParsePlatformProfile(raw []byte) (PlatformProfile, error) {
	platformSchema, _, err := schemas()
	if err != nil {
		return PlatformProfile{}, err
	}
	if err := validateJSON(platformSchema, raw, "Platform Profile"); err != nil {
		return PlatformProfile{}, err
	}
	var profile PlatformProfile
	if err := json.Unmarshal(raw, &profile); err != nil {
		return PlatformProfile{}, fmt.Errorf("解析 Platform Profile 字段: %w", err)
	}
	classes := make(map[string]struct{}, len(profile.ServiceClasses))
	for _, serviceClass := range profile.ServiceClasses {
		classes[serviceClass] = struct{}{}
	}
	for i := range profile.Attachments {
		attachment := &profile.Attachments[i]
		if _, ok := classes[attachment.ServiceClass]; !ok {
			return PlatformProfile{}, fmt.Errorf("Platform Profile attachment 使用未声明 serviceClass %q", attachment.ServiceClass)
		}
		seen := map[string]struct{}{}
		for j := range attachment.Plugins {
			plugin := &attachment.Plugins[j]
			if plugin.Channel == "" {
				plugin.Channel = "stable"
			}
			if _, duplicate := seen[plugin.ID]; duplicate {
				return PlatformProfile{}, fmt.Errorf("Platform Profile serviceClass %q 的插件 id 重复: %q", attachment.ServiceClass, plugin.ID)
			}
			seen[plugin.ID] = struct{}{}
		}
	}
	profile.Services, err = deploymentv2.NormalizeServiceUnits(profile.Services)
	if err != nil {
		return PlatformProfile{}, fmt.Errorf("Platform Profile services 无效: %w", err)
	}
	return profile, nil
}

func ParseApplicationComposition(raw []byte) (ApplicationComposition, error) {
	_, applicationSchema, err := schemas()
	if err != nil {
		return ApplicationComposition{}, err
	}
	if err := validateJSON(applicationSchema, raw, "Application Composition"); err != nil {
		return ApplicationComposition{}, err
	}
	var composition ApplicationComposition
	if err := json.Unmarshal(raw, &composition); err != nil {
		return ApplicationComposition{}, fmt.Errorf("解析 Application Composition 字段: %w", err)
	}
	units := make([]deploymentv2.ServiceUnit, len(composition.Units))
	for i := range composition.Units {
		units[i] = composition.Units[i].Spec
	}
	units, err = deploymentv2.NormalizeServiceUnits(units)
	if err != nil {
		return ApplicationComposition{}, fmt.Errorf("Application Composition units 无效: %w", err)
	}
	for i := range composition.Units {
		composition.Units[i].Spec = units[i]
	}
	return composition, nil
}

func ValidatePlatformProfile(profile PlatformProfile) (PlatformProfile, error) {
	raw, err := json.Marshal(profile)
	if err != nil {
		return PlatformProfile{}, fmt.Errorf("编码 Platform Profile: %w", err)
	}
	return ParsePlatformProfile(raw)
}

func ValidateApplicationComposition(composition ApplicationComposition) (ApplicationComposition, error) {
	raw, err := json.Marshal(composition)
	if err != nil {
		return ApplicationComposition{}, fmt.Errorf("编码 Application Composition: %w", err)
	}
	return ParseApplicationComposition(raw)
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
	raw, err := os.ReadFile(filename)
	if err != nil {
		return PlatformProfile{}, fmt.Errorf("读取 Platform Profile 文件: %w", err)
	}
	return ParsePlatformProfile(raw)
}

func ParseApplicationCompositionFile(filename string) (ApplicationComposition, error) {
	raw, err := os.ReadFile(filename)
	if err != nil {
		return ApplicationComposition{}, fmt.Errorf("读取 Application Composition 文件: %w", err)
	}
	return ParseApplicationComposition(raw)
}

func digest(value any) string {
	raw, _ := json.Marshal(value)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (p PlatformProfile) Digest() string        { return digest(p) }
func (c ApplicationComposition) Digest() string { return digest(c) }
