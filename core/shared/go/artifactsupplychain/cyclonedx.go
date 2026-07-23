// Package artifactsupplychain validates bounded, language-neutral supply-chain
// documents before they enter a trusted artifact catalog.
package artifactsupplychain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const (
	MaxCycloneDXBytes      = 16 << 20
	MaxCycloneDXComponents = 100_000
)

type CycloneDXSummary struct {
	SpecVersion  string
	SerialNumber string
	RootName     string
	RootVersion  string
	Components   int
	SHA256       string
}

type cycloneDXDocument struct {
	BOMFormat    string            `json:"bomFormat"`
	SpecVersion  string            `json:"specVersion"`
	SerialNumber string            `json:"serialNumber"`
	Version      int               `json:"version"`
	Metadata     cycloneDXMetadata `json:"metadata"`
	Components   []json.RawMessage `json:"components"`
}

type cycloneDXMetadata struct {
	Component cycloneDXComponent `json:"component"`
}

type cycloneDXComponent struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

func InspectCycloneDX(raw []byte) (CycloneDXSummary, error) {
	if len(raw) == 0 || len(raw) > MaxCycloneDXBytes {
		return CycloneDXSummary{}, fmt.Errorf("CycloneDX SBOM 大小必须在 1..%d 字节内", MaxCycloneDXBytes)
	}
	var document cycloneDXDocument
	if err := json.Unmarshal(raw, &document); err != nil {
		return CycloneDXSummary{}, errors.New("CycloneDX SBOM 不是有效 JSON")
	}
	if document.BOMFormat != "CycloneDX" || (document.SpecVersion != "1.5" && document.SpecVersion != "1.6") || document.Version < 1 {
		return CycloneDXSummary{}, errors.New("只接受 CycloneDX JSON 1.5 或 1.6")
	}
	document.Metadata.Component.Name = strings.TrimSpace(document.Metadata.Component.Name)
	document.Metadata.Component.Version = strings.TrimSpace(document.Metadata.Component.Version)
	if document.Metadata.Component.Name == "" || document.Metadata.Component.Version == "" {
		return CycloneDXSummary{}, errors.New("CycloneDX SBOM 必须声明 metadata.component 名称与版本")
	}
	if len(document.Components) > MaxCycloneDXComponents {
		return CycloneDXSummary{}, fmt.Errorf("CycloneDX SBOM 组件数超过上限 %d", MaxCycloneDXComponents)
	}
	for index, rawComponent := range document.Components {
		var component cycloneDXComponent
		if err := json.Unmarshal(rawComponent, &component); err != nil || strings.TrimSpace(component.Name) == "" {
			return CycloneDXSummary{}, fmt.Errorf("CycloneDX SBOM 组件 %d 缺少有效名称", index)
		}
	}
	digest := sha256.Sum256(raw)
	return CycloneDXSummary{
		SpecVersion: document.SpecVersion, SerialNumber: document.SerialNumber,
		RootName: document.Metadata.Component.Name, RootVersion: document.Metadata.Component.Version,
		Components: len(document.Components), SHA256: hex.EncodeToString(digest[:]),
	}, nil
}
