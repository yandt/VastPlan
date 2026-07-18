package edge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifacttrust"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginid"
	"cdsoft.com.cn/VastPlan/core/shared/go/portalapi"
)

const maxFrontendModuleBytes = int64(32 << 20)

type FrontendModuleAsset struct {
	Descriptor portalapi.FrontendModule
	Content    []byte
}

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

func (c *TrustedCatalog) ValidatePortal(ctx context.Context, tenantID string, spec portalapi.PortalSpec) error {
	if spec.Revision == 0 || tenantID == "" || spec.TenantID != tenantID {
		return errors.New("Portal 解析结果的 revision 或 tenant 无效")
	}
	if !validCompositionRef(spec.Resolution.PlatformProfile) || !validCompositionRef(spec.Resolution.ApplicationComposition) {
		return errors.New("Portal 解析结果缺少有效输入锁")
	}
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
		origin, ok := spec.Resolution.PluginOrigins[ref.ID]
		if !ok {
			return fmt.Errorf("Portal 插件 %s 缺少解析来源", ref.ID)
		}
		if err := compositioncommonv1.ValidateOrigin(origin); err != nil {
			return err
		}
		class, err := pluginid.ClassifyManagement(manifest.ID, manifest.Publisher)
		if err != nil {
			return err
		}
		if origin == compositioncommonv1.OriginApplication && class == pluginid.ManagementPlatform {
			return fmt.Errorf("Frontend Application Composition 不能选择平台插件 %s", ref.ID)
		}
		if class == pluginid.ManagementDevelopment {
			return fmt.Errorf("Portal v1 不允许开发插件 %s", ref.ID)
		}
		isSelectedDesignSystem := ref.ID == spec.DesignSystem.ID && ref.Version == spec.DesignSystem.Version && channel(ref.Channel) == channel(spec.DesignSystem.Channel)
		var frontendContributions struct {
			DesignSystems []json.RawMessage `json:"designSystems"`
		}
		if err := json.Unmarshal(manifest.Contributes["frontend"], &frontendContributions); err != nil {
			return fmt.Errorf("解析插件 %s 前端贡献: %w", ref.ID, err)
		}
		if len(frontendContributions.DesignSystems) > 0 && !isSelectedDesignSystem {
			return fmt.Errorf("Portal 不允许第二个设计系统插件 %s", ref.ID)
		}
		if isSelectedDesignSystem {
			selected = manifest
		}
	}
	if len(seen) != len(spec.Resolution.PluginOrigins) {
		return errors.New("Portal 解析来源包含未部署插件")
	}
	if spec.Resolution.PluginOrigins[spec.DesignSystem.ID] != compositioncommonv1.OriginPlatformProfile {
		return errors.New("Portal 设计系统必须来自 Platform Profile")
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

// ResolveRuntime converts a governed PortalSpec into the only bootstrap
// document accepted by the browser. Every module digest is calculated from a
// freshly verified package, never from a manifest claim.
func (c *TrustedCatalog) ResolveRuntime(ctx context.Context, tenantID string, spec portalapi.PortalSpec) (portalapi.RuntimeSpec, error) {
	if err := c.ValidatePortal(ctx, tenantID, spec); err != nil {
		return portalapi.RuntimeSpec{}, err
	}
	runtime := portalapi.RuntimeSpec{Portal: spec, Modules: make([]portalapi.FrontendModule, 0, len(spec.Plugins))}
	for _, ref := range spec.Plugins {
		asset, err := c.frontendModule(ctx, spec.Revision, ref)
		if err != nil {
			return portalapi.RuntimeSpec{}, err
		}
		runtime.Modules = append(runtime.Modules, asset.Descriptor)
	}
	return runtime, nil
}

// ReadFrontendModule serves only a module locked into the supplied active
// revision. A caller cannot turn the asset endpoint into an arbitrary artifact
// reader by choosing its own plugin version or entry path.
func (c *TrustedCatalog) ReadFrontendModule(ctx context.Context, tenantID string, spec portalapi.PortalSpec, pluginID string) (FrontendModuleAsset, error) {
	if err := c.ValidatePortal(ctx, tenantID, spec); err != nil {
		return FrontendModuleAsset{}, err
	}
	for _, ref := range spec.Plugins {
		if ref.ID == pluginID {
			return c.frontendModule(ctx, spec.Revision, ref)
		}
	}
	return FrontendModuleAsset{}, fmt.Errorf("Portal revision 未锁定前端插件 %s", pluginID)
}

func (c *TrustedCatalog) frontendModule(ctx context.Context, revision uint64, ref portalapi.PluginRef) (FrontendModuleAsset, error) {
	artifact, packageBytes, manifest, err := c.verifiedManifest(ctx, ref)
	if err != nil {
		return FrontendModuleAsset{}, err
	}
	entry := manifest.Entry["frontend"]
	if entry == "" || (!strings.HasSuffix(entry, ".js") && !strings.HasSuffix(entry, ".mjs")) {
		return FrontendModuleAsset{}, fmt.Errorf("插件 %s 缺少已构建的 JavaScript frontend 入口", ref.ID)
	}
	content, err := artifacttrust.ReadPackageFile(packageBytes, entry, maxFrontendModuleBytes)
	if err != nil {
		return FrontendModuleAsset{}, fmt.Errorf("读取插件 %s frontend 入口: %w", ref.ID, err)
	}
	digest := sha256.Sum256(content)
	return FrontendModuleAsset{
		Descriptor: portalapi.FrontendModule{
			PluginRef: ref,
			Entry:     entry,
			URL:       fmt.Sprintf("/v1/portal-modules/%d/%s.js", revision, ref.ID),
			SHA256:    hex.EncodeToString(digest[:]), PackageSHA256: artifact.SHA256,
		},
		Content: content,
	}, nil
}

func validCompositionRef(ref compositioncommonv1.Ref) bool {
	if ref.ID == "" || ref.Revision == 0 || len(ref.Digest) != 64 {
		return false
	}
	_, err := hex.DecodeString(ref.Digest)
	return err == nil
}

func (c *TrustedCatalog) manifest(ctx context.Context, ref portalapi.PluginRef) (pluginv1.Manifest, error) {
	_, _, manifest, err := c.verifiedManifest(ctx, ref)
	return manifest, err
}

func (c *TrustedCatalog) verifiedManifest(ctx context.Context, ref portalapi.PluginRef) (pluginv1.Artifact, []byte, pluginv1.Manifest, error) {
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
			return pluginv1.Artifact{}, nil, pluginv1.Manifest{}, fmt.Errorf("已验证制品清单无效: %w", err)
		}
		return artifact, envelope.PackageBytes, manifest, nil
	}
	return pluginv1.Artifact{}, nil, pluginv1.Manifest{}, fmt.Errorf("无法取得并验证 %s@%s: %w", ref.ID, ref.Version, last)
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
