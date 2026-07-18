// Package frontendcompositionv1 defines the two authorized inputs for a
// Frontend Portal composition. Resolved Portal revisions live in portalapi.
package frontendcompositionv1

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"

	commonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/common/v1"
	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
)

const (
	PlatformProfileSchemaURL        = "https://schemas.cdsoft.com.cn/vastplan/composition/frontend/v1/vastplan.platform-profile.schema.json"
	ApplicationCompositionSchemaURL = "https://schemas.cdsoft.com.cn/vastplan/composition/frontend/v1/vastplan.application-composition.schema.json"
)

//go:embed vastplan.platform-profile.schema.json
var platformSchemaJSON []byte

//go:embed vastplan.application-composition.schema.json
var applicationSchemaJSON []byte

var compileOnce sync.Once
var platformSchema, applicationSchema *jsonschema.Schema
var compileErr error

type PluginRef struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Channel string `json:"channel,omitempty"`
}

type DesignSystem struct {
	PluginRef
	UIContract string `json:"uiContract"`
}

type SecurityPolicy struct {
	FirstPartyOnly   bool `json:"firstPartyOnly"`
	RequireIntegrity bool `json:"requireIntegrity"`
}

type PlatformProfile struct {
	compositioncommonv1.Document
	Target       compositioncommonv1.Target `json:"target"`
	DesignSystem DesignSystem               `json:"designSystem"`
	Plugins      []PluginRef                `json:"plugins"`
	Security     SecurityPolicy             `json:"security,omitempty"`
}

type ApplicationComposition struct {
	compositioncommonv1.Document
	Target   compositioncommonv1.Target `json:"target"`
	Route    string                     `json:"route"`
	Domains  []string                   `json:"domains,omitempty"`
	Audience []string                   `json:"audience,omitempty"`
	Branding map[string]any             `json:"branding,omitempty"`
	Plugins  []PluginRef                `json:"plugins"`
	Config   map[string]any             `json:"config,omitempty"`
}

func schemas() (*jsonschema.Schema, *jsonschema.Schema, error) {
	compileOnce.Do(func() {
		compiler := jsonschema.NewCompiler()
		if err := commonv1.AddResources(compiler); err != nil {
			compileErr = err
			return
		}
		if err := compositioncommonv1.AddResources(compiler); err != nil {
			compileErr = err
			return
		}
		for _, resource := range []struct {
			url string
			raw []byte
		}{{PlatformProfileSchemaURL, platformSchemaJSON}, {ApplicationCompositionSchemaURL, applicationSchemaJSON}} {
			doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(resource.raw))
			if err != nil {
				compileErr = err
				return
			}
			if err := compiler.AddResource(resource.url, doc); err != nil {
				compileErr = err
				return
			}
		}
		platformSchema, compileErr = compiler.Compile(PlatformProfileSchemaURL)
		if compileErr != nil {
			return
		}
		applicationSchema, compileErr = compiler.Compile(ApplicationCompositionSchemaURL)
	})
	return platformSchema, applicationSchema, compileErr
}

func ParsePlatformProfile(raw []byte) (PlatformProfile, error) {
	p, _, err := schemas()
	if err != nil {
		return PlatformProfile{}, err
	}
	if err := validateJSON(p, raw, "Frontend Platform Profile"); err != nil {
		return PlatformProfile{}, err
	}
	var value PlatformProfile
	if err := json.Unmarshal(raw, &value); err != nil {
		return PlatformProfile{}, err
	}
	if err := compositioncommonv1.ValidateTarget(value.Target, compositioncommonv1.KernelFrontend); err != nil {
		return PlatformProfile{}, err
	}
	value.Plugins, err = normalizeRefs(value.Plugins)
	if err != nil {
		return PlatformProfile{}, err
	}
	value.DesignSystem.Channel = channel(value.DesignSystem.Channel)
	found := false
	for _, ref := range value.Plugins {
		if same(ref, value.DesignSystem.PluginRef) {
			found = true
		}
	}
	if !found {
		return PlatformProfile{}, fmt.Errorf("Frontend Platform Profile plugins 必须精确包含设计系统")
	}
	return value, nil
}

func ParseApplicationComposition(raw []byte) (ApplicationComposition, error) {
	_, a, err := schemas()
	if err != nil {
		return ApplicationComposition{}, err
	}
	if err := validateJSON(a, raw, "Frontend Application Composition"); err != nil {
		return ApplicationComposition{}, err
	}
	var value ApplicationComposition
	if err := json.Unmarshal(raw, &value); err != nil {
		return ApplicationComposition{}, err
	}
	if err := compositioncommonv1.ValidateTarget(value.Target, compositioncommonv1.KernelFrontend); err != nil {
		return ApplicationComposition{}, err
	}
	value.Plugins, err = normalizeRefs(value.Plugins)
	if err != nil {
		return ApplicationComposition{}, err
	}
	return value, nil
}

func ValidatePlatformProfile(v PlatformProfile) (PlatformProfile, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return PlatformProfile{}, err
	}
	return ParsePlatformProfile(raw)
}
func ValidateApplicationComposition(v ApplicationComposition) (ApplicationComposition, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return ApplicationComposition{}, err
	}
	return ParseApplicationComposition(raw)
}
func ParsePlatformProfileFile(path string) (PlatformProfile, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return PlatformProfile{}, err
	}
	return ParsePlatformProfile(raw)
}
func ParseApplicationCompositionFile(path string) (ApplicationComposition, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ApplicationComposition{}, err
	}
	return ParseApplicationComposition(raw)
}
func (v PlatformProfile) Digest() string        { return compositioncommonv1.Digest(v) }
func (v ApplicationComposition) Digest() string { return compositioncommonv1.Digest(v) }

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
func normalizeRefs(refs []PluginRef) ([]PluginRef, error) {
	out := make([]PluginRef, len(refs))
	copy(out, refs)
	seen := map[string]struct{}{}
	for i := range out {
		out[i].Channel = channel(out[i].Channel)
		if _, ok := seen[out[i].ID]; ok {
			return nil, fmt.Errorf("Frontend 组合插件 id 重复: %q", out[i].ID)
		}
		seen[out[i].ID] = struct{}{}
	}
	return out, nil
}
func channel(v string) string {
	if v == "" {
		return "stable"
	}
	return v
}
func same(a, b PluginRef) bool {
	return a.ID == b.ID && a.Version == b.Version && channel(a.Channel) == channel(b.Channel)
}
