package edge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	pluginv1 "cdsoft.com.cn/VastPlan/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/shared/go/artifacttrust"
	"cdsoft.com.cn/VastPlan/shared/go/pluginid"
	"cdsoft.com.cn/VastPlan/shared/go/portalapi"
)

// TrustedCatalog reuses the kernel artifact-verification boundary. An artifact
// source cannot make itself trusted: every candidate passes ArtifactVerifier
// before its manifest is considered for a Portal composition.
type TrustedCatalog struct {
	sources  []ArtifactSource
	verifier ArtifactVerifier
}

// ArtifactSource and ArtifactVerifier are stable Edge ports. The Backend
// composition root adapts Node Agent's verifier here; Edge never imports a
// sibling implementation package.
type ArtifactSource interface {
	Fetch(context.Context, pluginv1.ArtifactRef) (artifacttrust.Envelope, error)
}
type ArtifactVerifier interface {
	Verify(context.Context, pluginv1.ArtifactRef, artifacttrust.Envelope) (pluginv1.Artifact, error)
}

func NewTrustedCatalog(sources []ArtifactSource, verifier ArtifactVerifier) (*TrustedCatalog, error) {
	if len(sources) == 0 {
		return nil, errors.New("Portal Catalog 至少需要一个制品来源")
	}
	if verifier == nil {
		return nil, errors.New("Portal Catalog 必须注入内核制品验证器")
	}
	return &TrustedCatalog{sources: append([]ArtifactSource(nil), sources...), verifier: verifier}, nil
}

func (c *TrustedCatalog) ValidatePortal(ctx context.Context, _ string, spec portalapi.PortalSpec) error {
	if !pluginid.IsFirstPartyID(spec.DesignSystem.ID) {
		return errors.New("Portal 设计系统必须是第一方插件")
	}
	seen := map[string]struct{}{}
	var selected pluginv1.Manifest
	for _, ref := range spec.Plugins {
		if !pluginid.IsFirstPartyID(ref.ID) {
			return fmt.Errorf("Portal v1 不允许第三方前端插件: %s", ref.ID)
		}
		key := ref.ID + "@" + ref.Version + "/" + channel(ref.Channel)
		if _, duplicate := seen[key]; duplicate {
			return fmt.Errorf("Portal 插件引用重复: %s", key)
		}
		seen[key] = struct{}{}
		manifest, err := c.manifest(ctx, ref)
		if err != nil {
			return err
		}
		if manifest.Engines["frontend"] == "" {
			return fmt.Errorf("插件 %s 未声明 frontend engine", ref.ID)
		}
		if ref.ID == spec.DesignSystem.ID && ref.Version == spec.DesignSystem.Version && channel(ref.Channel) == channel(spec.DesignSystem.Channel) {
			selected = manifest
		}
	}
	if selected.ID == "" {
		return errors.New("所选设计系统不在 Portal plugins 中")
	}
	var contribution struct {
		DesignSystems []struct {
			ID           string   `json:"id"`
			UIContract   string   `json:"uiContract"`
			Framework    string   `json:"framework"`
			Capabilities []string `json:"capabilities"`
		} `json:"designSystems"`
	}
	if err := json.Unmarshal(selected.Contributes["frontend"], &contribution); err != nil {
		return fmt.Errorf("解析设计系统贡献: %w", err)
	}
	for _, ds := range contribution.DesignSystems {
		if ds.ID == "ui.design-system" && ds.UIContract == spec.DesignSystem.UIContract && ds.Framework != "" && completeCapabilities(ds.Capabilities) {
			return nil
		}
	}
	return errors.New("设计系统未提供匹配且完整的 ui.design-system 贡献")
}

func (c *TrustedCatalog) manifest(ctx context.Context, ref portalapi.PluginRef) (pluginv1.Manifest, error) {
	artifactRef := pluginv1.ArtifactRef{PluginID: ref.ID, Version: ref.Version, Channel: channel(ref.Channel)}
	var last error
	for _, source := range c.sources {
		envelope, err := source.Fetch(ctx, artifactRef)
		if err != nil {
			last = err
			continue
		}
		artifact, err := c.verifier.Verify(ctx, artifactRef, envelope)
		if err != nil {
			last = err
			continue
		}
		manifest, err := pluginv1.ParseManifest(artifact.Manifest)
		if err != nil {
			return pluginv1.Manifest{}, fmt.Errorf("已验证制品清单无效: %w", err)
		}
		return manifest, nil
	}
	return pluginv1.Manifest{}, fmt.Errorf("无法取得并验证 %s@%s: %w", ref.ID, ref.Version, last)
}

func channel(value string) string {
	if strings.TrimSpace(value) == "" {
		return "stable"
	}
	return value
}
func completeCapabilities(values []string) bool {
	required := map[string]bool{"layout": false, "menu": false, "overlay": false, "form": false, "data": false, "feedback": false, "theme": false}
	for _, v := range values {
		if _, ok := required[v]; ok {
			required[v] = true
		}
	}
	for _, ok := range required {
		if !ok {
			return false
		}
	}
	return true
}
