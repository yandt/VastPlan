package repositoryruntime

import (
	"errors"
	"slices"
	"strings"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

type SupplyChainPolicy struct {
	RequiredSBOMChannels []string `json:"requiredSBOMChannels"`
}

func (p SupplyChainPolicy) normalized() SupplyChainPolicy {
	if p.RequiredSBOMChannels == nil {
		p.RequiredSBOMChannels = []string{"stable"}
	}
	channels := make([]string, len(p.RequiredSBOMChannels))
	copy(channels, p.RequiredSBOMChannels)
	p.RequiredSBOMChannels = channels
	return p
}

func (p SupplyChainPolicy) Validate() error {
	p = p.normalized()
	if len(p.RequiredSBOMChannels) > 16 {
		return errors.New("SBOM 强制 channel 不能超过 16 个")
	}
	for index, channel := range p.RequiredSBOMChannels {
		channel = strings.TrimSpace(channel)
		if channel == "" || channel != p.RequiredSBOMChannels[index] || slices.Contains(p.RequiredSBOMChannels[:index], channel) {
			return errors.New("SBOM 强制 channel 必须非空、规范且不重复")
		}
	}
	return nil
}

func (p SupplyChainPolicy) requiresSBOM(channel string) bool {
	return slices.Contains(p.normalized().RequiredSBOMChannels, channel)
}

func (p SupplyChainPolicy) admit(artifact pluginv1.Artifact) error {
	if !p.requiresSBOM(artifact.Channel) {
		return nil
	}
	manifest, err := pluginv1.ParseManifest(artifact.Manifest)
	if err != nil {
		return err
	}
	if manifest.SupplyChain == nil || manifest.SupplyChain.SBOM == nil {
		return errors.New("该发布 channel 强制要求签名清单绑定 CycloneDX SBOM")
	}
	return nil
}
