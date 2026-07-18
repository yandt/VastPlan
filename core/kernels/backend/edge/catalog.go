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
	frontendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/frontend/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifacttrust"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginid"
	"cdsoft.com.cn/VastPlan/core/shared/go/portalapi"
)

const maxFrontendModuleBytes = int64(32 << 20)

type FrontendModuleAsset struct {
	Descriptor  portalapi.FrontendModule
	Content     []byte
	GzipContent []byte
}

type verifiedPortalPlugin struct {
	ref          portalapi.PluginRef
	artifact     pluginv1.Artifact
	packageBytes []byte
	manifest     pluginv1.Manifest
}

// TrustedCatalog reuses the kernel artifact-verification boundary. An artifact
// source cannot make itself trusted: every candidate passes ArtifactVerifier
// before its manifest is considered for a Portal composition.
type TrustedCatalog struct {
	sources  []ArtifactSource
	verifier ArtifactVerifier
	delivery *frontendDeliveryStore
	origin   *frontendDeliveryStore
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

type TrustedCatalogOption func(*TrustedCatalog) error

func WithFrontendDeliveryRoot(root string) TrustedCatalogOption {
	return func(c *TrustedCatalog) error {
		store, err := newFrontendDeliveryStore(root)
		c.delivery, c.origin = store, store
		return err
	}
}

// WithFrontendDeliveryDistribution separates the shared, trusted publication
// origin from this Portal Edge node's private local cache. Only Portal Edge
// nodes receive the cache; ordinary Backend service nodes never pull browser
// snapshots.
func WithFrontendDeliveryDistribution(originRoot, cacheRoot string) TrustedCatalogOption {
	return func(c *TrustedCatalog) error {
		if strings.TrimSpace(originRoot) == "" || strings.TrimSpace(cacheRoot) == "" {
			return errors.New("Portal 交付 origin 与 cache 路径不能为空")
		}
		origin, err := newFrontendDeliveryStore(originRoot)
		if err != nil {
			return err
		}
		cache, err := newFrontendDeliveryStore(cacheRoot)
		if err != nil {
			return err
		}
		c.origin, c.delivery = origin, cache
		return nil
	}
}

func NewTrustedCatalog(sources []ArtifactSource, verifier ArtifactVerifier, options ...TrustedCatalogOption) (*TrustedCatalog, error) {
	if len(sources) == 0 {
		return nil, errors.New("Portal Catalog 至少需要一个制品来源")
	}
	if verifier == nil {
		return nil, errors.New("Portal Catalog 必须注入内核制品验证器")
	}
	store, err := newFrontendDeliveryStore("")
	if err != nil {
		return nil, err
	}
	catalog := &TrustedCatalog{sources: append([]ArtifactSource(nil), sources...), verifier: verifier, delivery: store, origin: store}
	for _, option := range options {
		if err := option(catalog); err != nil {
			return nil, err
		}
	}
	return catalog, nil
}

func (c *TrustedCatalog) ValidatePortal(ctx context.Context, tenantID string, spec portalapi.PortalSpec) error {
	_, err := c.verifyPortal(ctx, tenantID, spec)
	return err
}

// verifyPortal is the single package-fetch and signature-verification pass used
// by publication. The returned immutable inputs are consumed directly by
// materialization, so an artifact is never downloaded again between trust
// validation and snapshot creation.
func (c *TrustedCatalog) verifyPortal(ctx context.Context, tenantID string, spec portalapi.PortalSpec) ([]verifiedPortalPlugin, error) {
	if spec.Revision == 0 || tenantID == "" || spec.TenantID != tenantID {
		return nil, errors.New("Portal 解析结果的 revision 或 tenant 无效")
	}
	if !validCompositionRef(spec.Resolution.PlatformCatalog) || !validCompositionRef(spec.Resolution.PlatformProfile) || !validCompositionRef(spec.Resolution.ApplicationComposition) {
		return nil, errors.New("Portal 解析结果缺少有效输入锁")
	}
	if err := frontendcompositionv1.ValidatePortalBinding(spec.Management); err != nil {
		return nil, fmt.Errorf("Portal 管理绑定无效: %w", err)
	}
	if spec.Management.TenantID != tenantID || spec.Management.PortalID != spec.ID || spec.Management.PlatformProfile != spec.Resolution.PlatformProfile || compositioncommonv1.Digest(spec.Management) != spec.Resolution.ManagementBindingDigest {
		return nil, errors.New("Portal 管理绑定与解析锁不一致")
	}
	if !pluginid.IsFirstPartyID(spec.DesignSystem.ID) {
		return nil, errors.New("Portal 设计系统必须是第一方插件")
	}
	if !pluginid.IsFirstPartyID(spec.Composition.ID) || !pluginid.IsFirstPartyID(spec.Layout.ID) {
		return nil, errors.New("Portal Shell 组合与布局必须是第一方插件")
	}
	if spec.DesignSystem.ID == spec.Composition.ID || spec.DesignSystem.ID == spec.Layout.ID || spec.Composition.ID == spec.Layout.ID {
		return nil, errors.New("Portal 设计系统、Shell 组合与布局必须来自独立插件")
	}
	seen := map[string]struct{}{}
	selected := map[string]pluginv1.Manifest{}
	verified := make([]verifiedPortalPlugin, 0, len(spec.Plugins))
	for _, ref := range spec.Plugins {
		if !pluginid.IsFirstPartyID(ref.ID) {
			return nil, fmt.Errorf("Portal v1 不允许第三方前端插件: %s", ref.ID)
		}
		key := ref.ID + "@" + ref.Version + "/" + channel(ref.Channel)
		if _, duplicate := seen[key]; duplicate {
			return nil, fmt.Errorf("Portal 插件引用重复: %s", key)
		}
		seen[key] = struct{}{}
		artifact, packageBytes, manifest, err := c.verifiedManifest(ctx, ref)
		if err != nil {
			return nil, err
		}
		verified = append(verified, verifiedPortalPlugin{ref: ref, artifact: artifact, packageBytes: packageBytes, manifest: manifest})
		if manifest.Engines["frontend"] == "" {
			return nil, fmt.Errorf("插件 %s 未声明 frontend engine", ref.ID)
		}
		origin, ok := spec.Resolution.PluginOrigins[ref.ID]
		if !ok {
			return nil, fmt.Errorf("Portal 插件 %s 缺少解析来源", ref.ID)
		}
		if err := compositioncommonv1.ValidateOrigin(origin); err != nil {
			return nil, err
		}
		class, err := pluginid.ClassifyManagement(manifest.ID, manifest.Publisher)
		if err != nil {
			return nil, err
		}
		if origin == compositioncommonv1.OriginApplication && class == pluginid.ManagementPlatform {
			return nil, fmt.Errorf("Frontend Application Composition 不能选择平台插件 %s", ref.ID)
		}
		if class == pluginid.ManagementDevelopment {
			return nil, fmt.Errorf("Portal v1 不允许开发插件 %s", ref.ID)
		}
		isSelectedDesignSystem := samePortalRef(ref, spec.DesignSystem.PluginRef)
		isSelectedComposition := samePortalRef(ref, spec.Composition.PluginRef)
		isSelectedLayout := samePortalRef(ref, spec.Layout.PluginRef)
		var frontendContributions struct {
			DesignSystems     []json.RawMessage `json:"designSystems"`
			ShellCompositions []json.RawMessage `json:"shellCompositions"`
			ShellLayouts      []json.RawMessage `json:"shellLayouts"`
		}
		if err := json.Unmarshal(manifest.Contributes["frontend"], &frontendContributions); err != nil {
			return nil, fmt.Errorf("解析插件 %s 前端贡献: %w", ref.ID, err)
		}
		if len(frontendContributions.DesignSystems) > 0 && !isSelectedDesignSystem {
			return nil, fmt.Errorf("Portal 不允许第二个设计系统插件 %s", ref.ID)
		}
		if len(frontendContributions.ShellCompositions) > 0 && !isSelectedComposition {
			return nil, fmt.Errorf("Portal 不允许第二个 Shell 组合插件 %s", ref.ID)
		}
		if len(frontendContributions.ShellLayouts) > 0 && !isSelectedLayout {
			return nil, fmt.Errorf("Portal 不允许第二个 Shell 布局插件 %s", ref.ID)
		}
		if isSelectedDesignSystem {
			selected["design"] = manifest
		}
		if isSelectedComposition {
			selected["composition"] = manifest
		}
		if isSelectedLayout {
			selected["layout"] = manifest
		}
	}
	if len(seen) != len(spec.Resolution.PluginOrigins) {
		return nil, errors.New("Portal 解析来源包含未部署插件")
	}
	if spec.Resolution.PluginOrigins[spec.DesignSystem.ID] != compositioncommonv1.OriginPlatformProfile || spec.Resolution.PluginOrigins[spec.Composition.ID] != compositioncommonv1.OriginPlatformProfile || spec.Resolution.PluginOrigins[spec.Layout.ID] != compositioncommonv1.OriginPlatformProfile {
		return nil, errors.New("Portal 设计系统、Shell 组合与布局必须来自 Platform Profile")
	}
	if selected["design"].ID == "" || selected["composition"].ID == "" || selected["layout"].ID == "" {
		return nil, errors.New("所选设计系统、Shell 组合或布局不在 Portal plugins 中")
	}
	var contribution struct {
		DesignSystems []struct {
			ID           string   `json:"id"`
			UIContract   string   `json:"uiContract"`
			Framework    string   `json:"framework"`
			Capabilities []string `json:"capabilities"`
		} `json:"designSystems"`
	}
	if err := json.Unmarshal(selected["design"].Contributes["frontend"], &contribution); err != nil {
		return nil, fmt.Errorf("解析设计系统贡献: %w", err)
	}
	for _, ds := range contribution.DesignSystems {
		if ds.ID == "ui.design-system" && ds.UIContract == spec.DesignSystem.UIContract && ds.Framework != "" && completeCapabilities(ds.Capabilities) {
			if !hasShellFoundationContribution(selected["composition"], "shellCompositions", "ui.shell-composition", spec.Composition.UIContract) {
				return nil, errors.New("Shell 组合插件未提供匹配的 ui.shell-composition 贡献")
			}
			if !hasShellFoundationContribution(selected["layout"], "shellLayouts", "ui.shell-layout", spec.Layout.UIContract) {
				return nil, errors.New("Shell 布局插件未提供匹配的 ui.shell-layout 贡献")
			}
			return verified, nil
		}
	}
	return nil, errors.New("设计系统未提供匹配且完整的 ui.design-system 贡献")
}

func hasShellFoundationContribution(manifest pluginv1.Manifest, field, id, contract string) bool {
	var frontend map[string][]struct {
		ID         string `json:"id"`
		UIContract string `json:"uiContract"`
	}
	if json.Unmarshal(manifest.Contributes["frontend"], &frontend) != nil {
		return false
	}
	for _, contribution := range frontend[field] {
		if contribution.ID == id && contribution.UIContract == contract {
			return true
		}
	}
	return false
}

func samePortalRef(left, right portalapi.PluginRef) bool {
	return left.ID == right.ID && left.Version == right.Version && channel(left.Channel) == channel(right.Channel)
}

// MaterializePortal is the only transition from verified plugin packages to
// browser delivery objects. It runs before publication and never on a browser
// request path.
func (c *TrustedCatalog) MaterializePortal(ctx context.Context, tenantID string, spec portalapi.PortalSpec) error {
	verified, err := c.verifyPortal(ctx, tenantID, spec)
	if err != nil {
		return err
	}
	assets := make([]FrontendModuleAsset, 0, len(spec.Plugins))
	for _, plugin := range verified {
		asset, err := frontendModule(spec.Revision, plugin)
		if err != nil {
			return err
		}
		asset.GzipContent, err = gzipBytes(asset.Content)
		if err != nil {
			return fmt.Errorf("压缩插件 %s frontend 入口: %w", plugin.ref.ID, err)
		}
		assets = append(assets, asset)
	}
	if err := c.origin.put(tenantID, spec, assets); err != nil {
		return err
	}
	if c.origin == c.delivery {
		return nil
	}
	return c.delivery.prefetchFrom(c.origin, tenantID, spec)
}

// PrefetchPortal verifies a central immutable snapshot and every referenced
// content object before atomically exposing the revision in the local Edge
// cache. It never reads plugin packages or executes frontend code.
func (c *TrustedCatalog) PrefetchPortal(ctx context.Context, tenantID string, spec portalapi.PortalSpec) error {
	_ = ctx
	if runtime, err := c.delivery.runtime(tenantID, spec); err == nil {
		ready := true
		for _, module := range runtime.Modules {
			if _, err := c.delivery.module(tenantID, spec, module.SHA256); err != nil {
				ready = false
				break
			}
		}
		if ready {
			return nil
		}
	}
	if c.origin == c.delivery {
		_, err := c.delivery.runtime(tenantID, spec)
		return err
	}
	return c.delivery.prefetchFrom(c.origin, tenantID, spec)
}

// ResolveRuntime reads an immutable publication snapshot from the local Edge
// cache. A newly added Edge may cold-fill that cache from the trusted delivery
// origin, but it never falls back to package download, signature verification,
// or archive extraction on the browser request path.
func (c *TrustedCatalog) ResolveRuntime(ctx context.Context, tenantID string, spec portalapi.PortalSpec) (portalapi.RuntimeSpec, error) {
	runtime, err := c.delivery.runtime(tenantID, spec)
	if err == nil || c.origin == c.delivery {
		return runtime, err
	}
	if err := c.PrefetchPortal(ctx, tenantID, spec); err != nil {
		return portalapi.RuntimeSpec{}, fmt.Errorf("Portal Edge 本地快照不可用且冷预取失败: %w", err)
	}
	return c.delivery.runtime(tenantID, spec)
}

// ResolveRecoveryRuntime binds every historical module URL to both the current
// active revision and the server-selected fallback revision. The browser cannot
// turn recovery mode into an arbitrary historical artifact reader.
func (c *TrustedCatalog) ResolveRecoveryRuntime(ctx context.Context, tenantID string, activeRevision uint64, spec portalapi.PortalSpec) (portalapi.RuntimeSpec, error) {
	if activeRevision == 0 || spec.Revision == 0 || activeRevision == spec.Revision {
		return portalapi.RuntimeSpec{}, errors.New("Portal 恢复 revision 无效")
	}
	runtime, err := c.ResolveRuntime(ctx, tenantID, spec)
	if err != nil {
		return portalapi.RuntimeSpec{}, err
	}
	for i := range runtime.Modules {
		runtime.Modules[i].URL = fmt.Sprintf("/v1/portal-recovery-modules/%d/%d/%s.js", activeRevision, spec.Revision, runtime.Modules[i].SHA256)
	}
	return runtime, nil
}

// ReadFrontendModule serves only a module locked into the supplied active
// revision. A caller cannot turn the asset endpoint into an arbitrary artifact
// reader by choosing its own plugin version or entry path.
func (c *TrustedCatalog) ReadFrontendModule(ctx context.Context, tenantID string, spec portalapi.PortalSpec, digest string) (FrontendModuleAsset, error) {
	_ = ctx
	return c.delivery.module(tenantID, spec, digest)
}

func frontendModule(revision uint64, plugin verifiedPortalPlugin) (FrontendModuleAsset, error) {
	ref, artifact, packageBytes, manifest := plugin.ref, plugin.artifact, plugin.packageBytes, plugin.manifest
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
