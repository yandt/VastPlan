// Package cyclonedx provides the single deterministic CycloneDX document
// model used by kernel and plugin release tooling.
package cyclonedx

import (
	"crypto/sha1" // UUIDv5 specification algorithm; not used for security.
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

type Document struct {
	BOMFormat    string       `json:"bomFormat"`
	SpecVersion  string       `json:"specVersion"`
	Version      int          `json:"version"`
	SerialNumber string       `json:"serialNumber"`
	Metadata     Metadata     `json:"metadata"`
	Components   []Component  `json:"components"`
	Dependencies []Dependency `json:"dependencies,omitempty"`
}

type Metadata struct {
	Component Component `json:"component"`
}

type Component struct {
	Type       string     `json:"type"`
	BOMRef     string     `json:"bom-ref,omitempty"`
	Name       string     `json:"name"`
	Version    string     `json:"version,omitempty"`
	PURL       string     `json:"purl,omitempty"`
	Hashes     []Hash     `json:"hashes,omitempty"`
	Properties []Property `json:"properties,omitempty"`
}

type Hash struct {
	Alg     string `json:"alg"`
	Content string `json:"content"`
}

type Property struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type Dependency struct {
	Ref       string   `json:"ref"`
	DependsOn []string `json:"dependsOn"`
}

func Build(root Component, values []Component, seed []byte) (Document, error) {
	if err := normalizeComponent(&root); err != nil {
		return Document{}, fmt.Errorf("CycloneDX 根组件无效: %w", err)
	}
	byRef := make(map[string]Component, len(values))
	for _, component := range values {
		if err := normalizeComponent(&component); err != nil {
			return Document{}, err
		}
		if prior, exists := byRef[component.BOMRef]; exists {
			left, _ := json.Marshal(prior)
			right, _ := json.Marshal(component)
			if string(left) != string(right) {
				return Document{}, fmt.Errorf("CycloneDX 组件标识冲突: %s", component.BOMRef)
			}
			continue
		}
		byRef[component.BOMRef] = component
	}
	components := make([]Component, 0, len(byRef))
	for _, component := range byRef {
		components = append(components, component)
	}
	sort.Slice(components, func(i, j int) bool { return components[i].BOMRef < components[j].BOMRef })
	dependsOn := make([]string, len(components))
	for index := range components {
		dependsOn[index] = components[index].BOMRef
	}
	if len(seed) == 0 {
		seed, _ = json.Marshal(struct {
			Root       Component   `json:"root"`
			Components []Component `json:"components"`
		}{Root: root, Components: components})
	}
	digest := sha256.Sum256(seed)
	return Document{
		BOMFormat: "CycloneDX", SpecVersion: "1.5", Version: 1,
		SerialNumber: "urn:uuid:" + DeterministicUUID(digest),
		Metadata:     Metadata{Component: root}, Components: components,
		Dependencies: []Dependency{{Ref: root.BOMRef, DependsOn: dependsOn}},
	}, nil
}

func Marshal(document Document) ([]byte, error) {
	raw, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(raw, '\n'), nil
}

func DeterministicUUID(digest [sha256.Size]byte) string {
	namespace := [...]byte{0x6b, 0xa7, 0xb8, 0x11, 0x9d, 0xad, 0x11, 0xd1, 0x80, 0xb4, 0x00, 0xc0, 0x4f, 0xd4, 0x30, 0xc8}
	hasher := sha1.New()
	_, _ = hasher.Write(namespace[:])
	_, _ = hasher.Write(digest[:])
	value := hasher.Sum(nil)[:16]
	value[6] = (value[6] & 0x0f) | 0x50
	value[8] = (value[8] & 0x3f) | 0x80
	hexValue := hex.EncodeToString(value)
	return fmt.Sprintf("%s-%s-%s-%s-%s", hexValue[:8], hexValue[8:12], hexValue[12:16], hexValue[16:20], hexValue[20:])
}

func normalizeComponent(component *Component) error {
	component.Type = strings.TrimSpace(component.Type)
	component.Name = strings.TrimSpace(component.Name)
	component.Version = strings.TrimSpace(component.Version)
	component.PURL = strings.TrimSpace(component.PURL)
	component.BOMRef = strings.TrimSpace(component.BOMRef)
	if component.Type == "" || component.Name == "" || component.Version == "" {
		return errors.New("组件 type、name 和 version 均不能为空")
	}
	if component.BOMRef == "" {
		component.BOMRef = component.PURL
	}
	if component.BOMRef == "" {
		return errors.New("组件 bom-ref 或 purl 不能为空")
	}
	sort.Slice(component.Hashes, func(i, j int) bool {
		if component.Hashes[i].Alg != component.Hashes[j].Alg {
			return component.Hashes[i].Alg < component.Hashes[j].Alg
		}
		return component.Hashes[i].Content < component.Hashes[j].Content
	})
	sort.Slice(component.Properties, func(i, j int) bool {
		if component.Properties[i].Name != component.Properties[j].Name {
			return component.Properties[i].Name < component.Properties[j].Name
		}
		return component.Properties[i].Value < component.Properties[j].Value
	})
	return nil
}
