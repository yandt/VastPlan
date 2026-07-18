// Package appv1 defines the immutable Runner App Profile contract.
//
// App Profiles describe prebuilt client applications. They are referenced by
// deployment/v2, but deliberately do not enter the backend service scheduler.
package appv1

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

	commonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/common/v1"
)

const ProfileSchemaURL = "https://schemas.cdsoft.com.cn/vastplan/app/v1/vastplan.app-profile.schema.json"

//go:embed vastplan.app-profile.schema.json
var profileSchemaJSON []byte

var (
	compileOnce   sync.Once
	profileSchema *jsonschema.Schema
	compileErr    error
)

type PluginRef struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Channel string `json:"channel,omitempty"`
}

type Profile struct {
	Version      int            `json:"version"`
	Revision     uint64         `json:"revision"`
	ID           string         `json:"id"`
	TenantID     string         `json:"tenantId"`
	Runtime      string         `json:"runtime"`
	Targets      []string       `json:"targets"`
	Distribution string         `json:"distribution"`
	AssignedTo   []string       `json:"assignedTo"`
	Plugins      []PluginRef    `json:"plugins"`
	Config       map[string]any `json:"config,omitempty"`
}

func schema() (*jsonschema.Schema, error) {
	compileOnce.Do(func() {
		compiler := jsonschema.NewCompiler()
		if err := commonv1.AddResources(compiler); err != nil {
			compileErr = err
			return
		}
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(profileSchemaJSON))
		if err != nil {
			compileErr = fmt.Errorf("解析 App Profile Schema: %w", err)
			return
		}
		if err := compiler.AddResource(ProfileSchemaURL, doc); err != nil {
			compileErr = fmt.Errorf("登记 App Profile Schema: %w", err)
			return
		}
		profileSchema, compileErr = compiler.Compile(ProfileSchemaURL)
		if compileErr != nil {
			compileErr = fmt.Errorf("编译 App Profile Schema: %w", compileErr)
		}
	})
	return profileSchema, compileErr
}

func Parse(raw []byte) (Profile, error) {
	sch, err := schema()
	if err != nil {
		return Profile{}, err
	}
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return Profile{}, fmt.Errorf("解析 App Profile JSON: %w", err)
	}
	if err := sch.Validate(instance); err != nil {
		return Profile{}, fmt.Errorf("App Profile 不符合 Schema: %w", err)
	}
	var profile Profile
	if err := json.Unmarshal(raw, &profile); err != nil {
		return Profile{}, fmt.Errorf("解析 App Profile 字段: %w", err)
	}
	pluginIDs := map[string]struct{}{}
	for i := range profile.Plugins {
		plugin := &profile.Plugins[i]
		if plugin.Channel == "" {
			plugin.Channel = "stable"
		}
		if _, exists := pluginIDs[plugin.ID]; exists {
			return Profile{}, fmt.Errorf("App Profile 插件 id 重复: %q", plugin.ID)
		}
		pluginIDs[plugin.ID] = struct{}{}
	}
	return profile, nil
}

// Validate applies the same machine-readable contract to a Profile assembled
// in Go and returns its normalized representation. Callers must not maintain a
// second handwritten copy of the schema rules.
func Validate(profile Profile) (Profile, error) {
	raw, err := json.Marshal(profile)
	if err != nil {
		return Profile{}, fmt.Errorf("编码 App Profile 字段: %w", err)
	}
	return Parse(raw)
}

func ParseFile(filename string) (Profile, error) {
	raw, err := os.ReadFile(filename)
	if err != nil {
		return Profile{}, fmt.Errorf("读取 App Profile 文件: %w", err)
	}
	return Parse(raw)
}

func (p Profile) Digest() string {
	raw, _ := json.Marshal(p)
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}
