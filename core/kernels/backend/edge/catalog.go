package edge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
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
	sources   []ArtifactSource
	verifier  ArtifactVerifier
	delivery  *frontendDeliveryStore
	origin    *frontendDeliveryStore
	testIndex TestArtifactIndex
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

func WithTestArtifactIndex(index TestArtifactIndex) TrustedCatalogOption {
	return func(c *TrustedCatalog) error {
		if index == nil {
			return errors.New("Portal 测试制品索引不能为空")
		}
		c.testIndex = index
		return nil
	}
}

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
	if !pluginid.IsFirstPartyID(spec.RuntimeEngine.ID) {
		return nil, errors.New("Portal Runtime Engine 必须是第一方插件")
	}
	if !pluginid.IsFirstPartyID(spec.RenderAdapter.ID) {
		return nil, errors.New("Portal 设计系统必须是第一方插件")
	}
	if !pluginid.IsFirstPartyID(spec.Shell.ID) {
		return nil, errors.New("Portal Shell 必须是第一方插件")
	}
	if !pluginid.IsFirstPartyID(spec.Workbench.ID) {
		return nil, errors.New("Portal Workbench 必须是第一方插件")
	}
	foundationIDs := map[string]struct{}{}
	for _, id := range []string{spec.RuntimeEngine.ID, spec.RenderAdapter.ID, spec.Shell.ID, spec.Workbench.ID} {
		if _, exists := foundationIDs[id]; exists {
			return nil, errors.New("Portal Runtime Engine、设计系统、Shell 与 Workbench 必须来自独立插件")
		}
		foundationIDs[id] = struct{}{}
	}
	seen := map[string]struct{}{}
	selected := map[string]pluginv1.Manifest{}
	manifestsByID := map[string]pluginv1.Manifest{}
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
		manifestsByID[manifest.ID] = manifest
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
		isSelectedRuntimeEngine := samePortalRef(ref, spec.RuntimeEngine.PluginRef)
		isSelectedRenderAdapter := samePortalRef(ref, spec.RenderAdapter.PluginRef)
		isSelectedShell := samePortalRef(ref, spec.Shell.PluginRef)
		isSelectedWorkbench := samePortalRef(ref, spec.Workbench.PluginRef)
		var frontendContributions struct {
			RuntimeEngines []json.RawMessage `json:"runtimeEngines"`
			RenderAdapters []json.RawMessage `json:"renderAdapters"`
			Shells         []json.RawMessage `json:"shells"`
			Workbenches    []json.RawMessage `json:"workbenches"`
		}
		if err := json.Unmarshal(manifest.Contributes["frontend"], &frontendContributions); err != nil {
			return nil, fmt.Errorf("解析插件 %s 前端贡献: %w", ref.ID, err)
		}
		if len(frontendContributions.RuntimeEngines) > 0 && !isSelectedRuntimeEngine {
			return nil, fmt.Errorf("Portal 不允许第二个 Runtime Engine 插件 %s", ref.ID)
		}
		if len(frontendContributions.RenderAdapters) > 0 && !isSelectedRenderAdapter {
			return nil, fmt.Errorf("Portal 不允许第二个设计系统插件 %s", ref.ID)
		}
		if len(frontendContributions.Shells) > 0 && !isSelectedShell {
			return nil, fmt.Errorf("Portal 不允许第二个 Shell 插件 %s", ref.ID)
		}
		if len(frontendContributions.Workbenches) > 0 && !isSelectedWorkbench {
			return nil, fmt.Errorf("Portal 不允许第二个 Workbench 插件 %s", ref.ID)
		}
		if isSelectedRuntimeEngine {
			selected["engine"] = manifest
		}
		if isSelectedRenderAdapter {
			selected["design"] = manifest
		}
		if isSelectedShell {
			selected["shell"] = manifest
		}
		if isSelectedWorkbench {
			selected["workbench"] = manifest
		}
	}
	if len(seen) != len(spec.Resolution.PluginOrigins) {
		return nil, errors.New("Portal 解析来源包含未部署插件")
	}
	if spec.Resolution.PluginOrigins[spec.RuntimeEngine.ID] != compositioncommonv1.OriginPlatformProfile || spec.Resolution.PluginOrigins[spec.RenderAdapter.ID] != compositioncommonv1.OriginPlatformProfile || spec.Resolution.PluginOrigins[spec.Shell.ID] != compositioncommonv1.OriginPlatformProfile || spec.Resolution.PluginOrigins[spec.Workbench.ID] != compositioncommonv1.OriginPlatformProfile {
		return nil, errors.New("Portal Runtime Engine、设计系统、Shell 与 Workbench 必须来自 Platform Profile")
	}
	if selected["engine"].ID == "" || selected["design"].ID == "" || selected["shell"].ID == "" || selected["workbench"].ID == "" {
		return nil, errors.New("所选 Runtime Engine、设计系统、Shell 或 Workbench 不在 Portal plugins 中")
	}
	if !hasRuntimeEngineContribution(selected["engine"], spec.RuntimeEngine) {
		return nil, errors.New("Runtime Engine 插件未提供匹配且完整的 ui.runtime.engine 贡献")
	}
	var contribution struct {
		RenderAdapters []struct {
			ID           string   `json:"id"`
			UIContract   string   `json:"uiContract"`
			EngineFamily string   `json:"engineFamily"`
			Framework    string   `json:"framework"`
			Capabilities []string `json:"capabilities"`
		} `json:"renderAdapters"`
	}
	if err := json.Unmarshal(selected["design"].Contributes["frontend"], &contribution); err != nil {
		return nil, fmt.Errorf("解析设计系统贡献: %w", err)
	}
	for _, ds := range contribution.RenderAdapters {
		if ds.ID == "ui.render.adapter" && ds.UIContract == spec.RenderAdapter.UIContract && ds.EngineFamily == spec.RuntimeEngine.Family && ds.Framework != "" && completeCapabilities(ds.Capabilities) {
			if !hasShellFoundationContribution(selected["shell"], "shells", "ui.structure.shell", spec.Shell.UIContract, spec.RuntimeEngine.Family) {
				return nil, errors.New("Shell 插件未提供匹配的 ui.structure.shell 贡献")
			}
			if !hasShellFoundationContribution(selected["workbench"], "workbenches", "ui.workflow.workbench", spec.Workbench.UIContract, spec.RuntimeEngine.Family) {
				return nil, errors.New("Workbench 插件未提供匹配的 ui.workflow.workbench 贡献")
			}
			if err := validateShellLibraryCatalog(selected["shell"], manifestsByID, spec); err != nil {
				return nil, err
			}
			return verified, nil
		}
	}
	return nil, errors.New("设计系统未提供匹配且完整的 ui.render.adapter 贡献")
}

func hasRuntimeEngineContribution(manifest pluginv1.Manifest, selection portalapi.RuntimeEngine) bool {
	var frontend struct {
		RuntimeEngines []struct {
			ID             string   `json:"id"`
			Family         string   `json:"family"`
			EngineContract string   `json:"engineContract"`
			BrowserEntry   string   `json:"browserEntry"`
			Capabilities   []string `json:"capabilities"`
		} `json:"runtimeEngines"`
	}
	if json.Unmarshal(manifest.Contributes["frontend"], &frontend) != nil {
		return false
	}
	for _, engine := range frontend.RuntimeEngines {
		if engine.ID == "ui.runtime.engine" && engine.Family == selection.Family && engine.EngineContract == selection.EngineContract && engine.BrowserEntry == manifest.Entry["frontend"] && containsString(engine.Capabilities, "csr") && containsString(engine.Capabilities, "generation") {
			return true
		}
	}
	return false
}

func validateShellLibraryCatalog(shellManifest pluginv1.Manifest, manifests map[string]pluginv1.Manifest, spec portalapi.PortalSpec) error {
	var frontend struct {
		Shells []struct {
			ID         string `json:"id"`
			UIContract string `json:"uiContract"`
			Libraries  []struct {
				ID     string              `json:"id"`
				Module portalapi.PluginRef `json:"module"`
			} `json:"libraries"`
		} `json:"shells"`
	}
	if json.Unmarshal(shellManifest.Contributes["frontend"], &frontend) != nil {
		return errors.New("解析 Shell Library Catalog 失败")
	}
	var libraries []struct {
		ID     string              `json:"id"`
		Module portalapi.PluginRef `json:"module"`
	}
	for _, shell := range frontend.Shells {
		if shell.ID == "ui.structure.shell" && shell.UIContract == spec.Shell.UIContract {
			libraries = shell.Libraries
			break
		}
	}
	if len(libraries) == 0 {
		return errors.New("Shell Catalog 未声明已签名 Library 模块")
	}
	allowed := map[string]struct{}{}
	for _, id := range spec.Shell.Config.AllowedTemplates {
		allowed[id] = struct{}{}
	}
	seenIDs, seenModules := map[string]struct{}{}, map[string]struct{}{}
	for _, library := range libraries {
		key := library.Module.ID + "@" + library.Module.Version + "/" + channel(library.Module.Channel)
		if library.ID == "" || library.Module.ID == "" {
			return errors.New("Shell Catalog Library 身份不完整")
		}
		if _, duplicate := seenIDs[library.ID]; duplicate {
			return fmt.Errorf("Shell Catalog Library ID 重复: %s", library.ID)
		}
		if _, duplicate := seenModules[key]; duplicate {
			return fmt.Errorf("Shell Catalog Library 模块重复: %s", key)
		}
		seenIDs[library.ID], seenModules[key] = struct{}{}, struct{}{}
		if !containsPortalRef(spec.Plugins, library.Module) || spec.Resolution.PluginOrigins[library.Module.ID] != compositioncommonv1.OriginPlatformProfile {
			return fmt.Errorf("Shell Library 未由 Platform Profile 精确锁定: %s", library.ID)
		}
		manifest := manifests[library.Module.ID]
		if !manifestProvidesShellLibrary(manifest, library.ID, spec.Shell.UIContract) {
			return fmt.Errorf("Shell Library 清单与 Catalog 不一致: %s", library.ID)
		}
	}
	for id := range allowed {
		if _, ok := seenIDs[id]; !ok {
			return fmt.Errorf("Platform Profile 允许了 Shell Catalog 未声明的 Library: %s", id)
		}
	}
	return nil
}

func containsPortalRef(values []portalapi.PluginRef, target portalapi.PluginRef) bool {
	for _, value := range values {
		if samePortalRef(value, target) {
			return true
		}
	}
	return false
}

func manifestProvidesShellLibrary(manifest pluginv1.Manifest, id, contract string) bool {
	var frontend struct {
		Libraries []struct {
			ID         string `json:"id"`
			Shell      string `json:"shell"`
			UIContract string `json:"uiContract"`
		} `json:"shellLibraries"`
	}
	if json.Unmarshal(manifest.Contributes["frontend"], &frontend) != nil || len(frontend.Libraries) != 1 {
		return false
	}
	library := frontend.Libraries[0]
	return library.ID == id && library.Shell == "ui.structure.shell" && library.UIContract == contract
}

func hasShellFoundationContribution(manifest pluginv1.Manifest, field, id, contract, engineFamily string) bool {
	var frontend map[string][]struct {
		ID           string `json:"id"`
		UIContract   string `json:"uiContract"`
		EngineFamily string `json:"engineFamily"`
	}
	if json.Unmarshal(manifest.Contributes["frontend"], &frontend) != nil {
		return false
	}
	for _, contribution := range frontend[field] {
		if contribution.ID == id && contribution.UIContract == contract && contribution.EngineFamily == engineFamily {
			return true
		}
	}
	return false
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
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
func (c *TrustedCatalog) MaterializePortal(ctx context.Context, tenantID string, spec portalapi.PortalSpec) ([]pluginv1.ArtifactReference, error) {
	verified, err := c.verifyPortal(ctx, tenantID, spec)
	if err != nil {
		return nil, err
	}
	runtime, assets, references, err := materializeFrontendRuntime(spec, verified)
	if err != nil {
		return nil, err
	}
	if err := c.origin.put(tenantID, spec, runtime, assets); err != nil {
		return nil, err
	}
	if c.origin == c.delivery {
		return references, nil
	}
	if err := c.delivery.prefetchFrom(c.origin, tenantID, spec); err != nil {
		return nil, err
	}
	return references, nil
}

// PrefetchPortal verifies a central immutable snapshot and every referenced
// content object before atomically exposing the revision in the local Edge
// cache. It never reads plugin packages or executes frontend code.
func (c *TrustedCatalog) PrefetchPortal(ctx context.Context, tenantID string, spec portalapi.PortalSpec) error {
	_ = ctx
	if runtime, err := c.delivery.runtime(tenantID, spec); err == nil {
		ready := true
		for _, module := range runtimeFrontendObjects(runtime) {
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
// Activation and the server-selected fallback Activation. The browser cannot
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
		runtime.Modules[i].URL = recoveryFrontendObjectURL(activeRevision, spec.Revision, runtime.Modules[i].URL)
	}
	for graphIndex := range runtime.ModuleGraphs {
		for nodeIndex := range runtime.ModuleGraphs[graphIndex].Nodes {
			node := &runtime.ModuleGraphs[graphIndex].Nodes[nodeIndex]
			node.URL = recoveryFrontendObjectURL(activeRevision, spec.Revision, node.URL)
		}
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
			Deferred: isDeferredFrontendModule(manifest), MediaType: "text/javascript",
		},
		Content: content,
	}, nil
}

func isDeferredFrontendModule(manifest pluginv1.Manifest) bool {
	var frontend struct {
		RendererModules []struct {
			ID         string `json:"id"`
			Adapter    string `json:"adapter"`
			UIContract string `json:"uiContract"`
			Framework  string `json:"framework"`
		} `json:"rendererModules"`
		ShellLibraries []struct {
			ID         string `json:"id"`
			Shell      string `json:"shell"`
			UIContract string `json:"uiContract"`
		} `json:"shellLibraries"`
	}
	if json.Unmarshal(manifest.Contributes["frontend"], &frontend) != nil {
		return false
	}
	if len(frontend.RendererModules) == 1 && frontend.RendererModules[0].ID != "" && frontend.RendererModules[0].Adapter == "ui.render.adapter" && frontend.RendererModules[0].UIContract != "" && frontend.RendererModules[0].Framework != "" {
		return true
	}
	return len(frontend.ShellLibraries) == 1 && frontend.ShellLibraries[0].ID != "" && frontend.ShellLibraries[0].Shell == "ui.structure.shell" && frontend.ShellLibraries[0].UIContract != ""
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
			if errors.Is(err, artifacttrust.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
				last = err
				continue
			}
			return pluginv1.Artifact{}, nil, pluginv1.Manifest{}, fmt.Errorf("读取 %s@%s 制品源失败: %w", ref.ID, ref.Version, err)
		}
		artifact, err := c.verifier.Verify(ctx, artifactRef, envelope)
		if err != nil {
			return pluginv1.Artifact{}, nil, pluginv1.Manifest{}, fmt.Errorf("验证 %s@%s 制品失败: %w", ref.ID, ref.Version, err)
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
