package edge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
	frontendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/frontend/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/pluginservice"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifacttrust"
	"cdsoft.com.cn/VastPlan/core/shared/go/portalapi"
)

type catalogSource map[string]artifacttrust.Envelope

func (s catalogSource) Fetch(_ context.Context, ref pluginv1.ArtifactRef) (artifacttrust.Envelope, error) {
	return s[ref.PluginID+"@"+ref.Version], nil
}

type countingCatalogSource struct {
	catalogSource
	calls int
}

type failingCatalogSource struct {
	err   error
	calls int
}

func (s *failingCatalogSource) Fetch(context.Context, pluginv1.ArtifactRef) (artifacttrust.Envelope, error) {
	s.calls++
	return artifacttrust.Envelope{}, s.err
}

func (s *countingCatalogSource) Fetch(ctx context.Context, ref pluginv1.ArtifactRef) (artifacttrust.Envelope, error) {
	s.calls++
	return s.catalogSource.Fetch(ctx, ref)
}

func TestTrustedCatalogFallsBackOnlyWhenExactSeedRefIsAbsent(t *testing.T) {
	manifestRaw := `{"id":"cn.vastplan.product.frontend.fallback","name":"fallback","description":"test","version":"1.0.0","publisher":"vastplan","engines":{"frontend":"^1.0"},"activation":["onPortalStartup"],"entry":{"frontend":"frontend/main.js"},"contributes":{"frontend":{"views":[]}}}`
	artifact, packageBytes := packageFrontendFixture(t, manifestRaw, []byte(`export default { register() {} };`))
	ref := portalapi.PluginRef{ID: artifact.PluginID, Version: artifact.Version, Channel: artifact.Channel}
	remote := &countingCatalogSource{catalogSource: catalogSource{artifact.PluginID + "@" + artifact.Version: {Artifact: artifact, PackageBytes: packageBytes}}}
	missing := &failingCatalogSource{err: artifacttrust.ErrNotFound}
	catalog, err := NewTrustedCatalog([]ArtifactSource{missing, remote}, contentVerifier{})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := catalog.verifiedManifest(context.Background(), ref); err != nil {
		t.Fatalf("本地精确 ref 不存在时应读取远端源: %v", err)
	}
	if missing.calls != 1 || remote.calls != 1 {
		t.Fatalf("制品源调用顺序错误: seed=%d remote=%d", missing.calls, remote.calls)
	}

	broken := &failingCatalogSource{err: errors.New("seed permission denied")}
	remote.calls = 0
	catalog, err = NewTrustedCatalog([]ArtifactSource{broken, remote}, contentVerifier{})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := catalog.verifiedManifest(context.Background(), ref); err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("本地读取异常必须 fail-closed: %v", err)
	}
	if remote.calls != 0 {
		t.Fatal("非 not-found 错误不得切换制品源")
	}
}

type contentVerifier struct{}

func (contentVerifier) Verify(_ context.Context, ref pluginv1.ArtifactRef, envelope artifacttrust.Envelope) (pluginv1.Artifact, error) {
	if envelope.Artifact.PluginID != ref.PluginID || envelope.Artifact.Version != ref.Version || envelope.Artifact.Channel != ref.Channel {
		return pluginv1.Artifact{}, os.ErrNotExist
	}
	if err := artifacttrust.ValidateContent(envelope.Artifact, envelope.PackageBytes); err != nil {
		return pluginv1.Artifact{}, err
	}
	return envelope.Artifact, nil
}

func TestTrustedCatalogRequiresVerifiedFrontendRenderAdapterContribution(t *testing.T) {
	dir := t.TempDir()
	module := []byte(`export default { id: "ui.render.adapter" };`)
	manifest := `{"id":"cn.vastplan.foundation.frontend.render.adapter.test","name":"test","description":"test","version":"1.0.0","publisher":"vastplan","engines":{"frontend":"^1.0"},"activation":["onPortalStartup"],"entry":{"frontend":"frontend/main.js"},"contributes":{"frontend":{"renderAdapters":[{"id":"ui.render.adapter","uiContract":"^4.0.0","framework":"test","capabilities":["layout","menu","overlay","form","data","feedback","theme"]}]}}}`
	if err := os.WriteFile(filepath.Join(dir, "vastplan.plugin.json"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "frontend"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "frontend", "main.js"), module, 0o600); err != nil {
		t.Fatal(err)
	}
	pkg, _, err := pluginservice.PackageDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := pluginservice.Describe("stable", pkg)
	if err != nil {
		t.Fatal(err)
	}
	source := catalogSource{artifact.PluginID + "@" + artifact.Version: {Artifact: artifact, PackageBytes: pkg}}
	standardArtifact, standardPackage := packageFrontendFixture(t, `{"id":"cn.vastplan.foundation.frontend.structure.layout.test-standard","name":"standard","description":"test","version":"1.0.0","publisher":"vastplan","engines":{"frontend":"^1.0"},"activation":["onPortalStartup"],"entry":{"frontend":"frontend/main.js"},"contributes":{"frontend":{"shellLibraries":[{"id":"standard","shell":"ui.structure.shell","uiContract":"^4.0.0"}]}}}`, []byte(`export const shellLibrary = { id: "standard" };`))
	topArtifact, topPackage := packageFrontendFixture(t, `{"id":"cn.vastplan.foundation.frontend.structure.layout.test-top","name":"top","description":"test","version":"1.0.0","publisher":"vastplan","engines":{"frontend":"^1.0"},"activation":["onPortalStartup"],"entry":{"frontend":"frontend/main.js"},"contributes":{"frontend":{"shellLibraries":[{"id":"top-navigation","shell":"ui.structure.shell","uiContract":"^4.0.0"}]}}}`, []byte(`export const shellLibrary = { id: "top-navigation" };`))
	shellArtifact, shellPackage := packageFrontendFixture(t, fmt.Sprintf(`{"id":"cn.vastplan.foundation.frontend.structure.shell.test","name":"shell","description":"test","version":"1.0.0","publisher":"vastplan","engines":{"frontend":"^1.0"},"activation":["onPortalStartup"],"entry":{"frontend":"frontend/main.js"},"contributes":{"frontend":{"shells":[{"id":"ui.structure.shell","uiContract":"^4.0.0","libraries":[{"id":"standard","module":{"id":%q,"version":"1.0.0","channel":"stable"}},{"id":"top-navigation","module":{"id":%q,"version":"1.0.0","channel":"stable"}}]}]}}}`, standardArtifact.PluginID, topArtifact.PluginID), []byte(`export default { id: "ui.structure.shell" };`))
	workbenchArtifact, workbenchPackage := packageFrontendFixture(t, `{"id":"cn.vastplan.foundation.frontend.workflow.workbench.test","name":"workbench","description":"test","version":"1.0.0","publisher":"vastplan","engines":{"frontend":"^1.0"},"activation":["onPortalStartup"],"entry":{"frontend":"frontend/main.js"},"contributes":{"frontend":{"workbenches":[{"id":"ui.workflow.workbench","uiContract":"^4.0.0"}]}}}`, []byte(`export default { id: "ui.workflow.workbench" };`))
	source[shellArtifact.PluginID+"@"+shellArtifact.Version] = artifacttrust.Envelope{Artifact: shellArtifact, PackageBytes: shellPackage}
	source[standardArtifact.PluginID+"@"+standardArtifact.Version] = artifacttrust.Envelope{Artifact: standardArtifact, PackageBytes: standardPackage}
	source[topArtifact.PluginID+"@"+topArtifact.Version] = artifacttrust.Envelope{Artifact: topArtifact, PackageBytes: topPackage}
	source[workbenchArtifact.PluginID+"@"+workbenchArtifact.Version] = artifacttrust.Envelope{Artifact: workbenchArtifact, PackageBytes: workbenchPackage}
	counted := &countingCatalogSource{catalogSource: source}
	deliveryRoot := t.TempDir()
	originRoot := filepath.Join(deliveryRoot, "origin")
	catalog, err := NewTrustedCatalog([]ArtifactSource{counted}, contentVerifier{}, WithFrontendDeliveryDistribution(originRoot, filepath.Join(deliveryRoot, "edge-a")))
	if err != nil {
		t.Fatal(err)
	}
	ref := portalapi.PluginRef{ID: artifact.PluginID, Version: artifact.Version}
	shellRef := portalapi.PluginRef{ID: shellArtifact.PluginID, Version: shellArtifact.Version}
	standardRef := portalapi.PluginRef{ID: standardArtifact.PluginID, Version: standardArtifact.Version}
	topRef := portalapi.PluginRef{ID: topArtifact.PluginID, Version: topArtifact.Version}
	workbenchRef := portalapi.PluginRef{ID: workbenchArtifact.PluginID, Version: workbenchArtifact.Version}
	spec := portalapi.PortalSpec{Revision: 1, ID: "admin", TenantID: "tenant-a", Route: "/", RenderAdapter: portalapi.RenderAdapter{PluginRef: ref, UIContract: "^4.0.0"}, Shell: portalapi.Shell{PluginRef: shellRef, UIContract: "^4.0.0", Config: frontendcompositionv1.ShellConfig{DefaultTemplate: "standard", AllowedTemplates: []string{"standard"}}}, Workbench: portalapi.Workbench{PluginRef: workbenchRef, UIContract: "^4.0.0"}, Plugins: []portalapi.PluginRef{ref, shellRef, standardRef, topRef, workbenchRef}, Resolution: portalapi.Resolution{PlatformProfile: compositioncommonv1.Ref{ID: "default", Revision: 1, Digest: strings.Repeat("a", 64)}, ApplicationComposition: compositioncommonv1.Ref{ID: "admin", Revision: 1, Digest: strings.Repeat("b", 64)}, PluginOrigins: map[string]string{ref.ID: compositioncommonv1.OriginPlatformProfile, shellRef.ID: compositioncommonv1.OriginPlatformProfile, standardRef.ID: compositioncommonv1.OriginPlatformProfile, topRef.ID: compositioncommonv1.OriginPlatformProfile, workbenchRef.ID: compositioncommonv1.OriginPlatformProfile}}}
	lockTestManagement(&spec)
	if err := catalog.ValidatePortal(context.Background(), "tenant-a", spec); err != nil {
		t.Fatalf("有效且已验证的设计系统应通过: %v", err)
	}
	beforeMaterialization := counted.calls
	references, err := catalog.MaterializePortal(context.Background(), "tenant-a", spec)
	if err != nil {
		t.Fatal(err)
	}
	if len(references) != len(spec.Plugins) || references[0].Ref.PluginID != artifact.PluginID || references[0].SHA256 != artifact.SHA256 || references[0].Ref.Channel != "stable" {
		t.Fatalf("物化结果必须返回已验签包的精确引用: %+v", references)
	}
	if got := counted.calls - beforeMaterialization; got != len(spec.Plugins) {
		t.Fatalf("物化期间每个制品应只获取和验证一次: got=%d want=%d", got, len(spec.Plugins))
	}
	materializationFetches := counted.calls
	runtime, err := catalog.ResolveRuntime(context.Background(), "tenant-a", spec)
	if err != nil {
		t.Fatalf("有效 Portal 应解析浏览器运行描述: %v", err)
	}
	wantDigest := sha256.Sum256(module)
	if len(runtime.Modules) != len(spec.Plugins) || runtime.Modules[0].SHA256 != hex.EncodeToString(wantDigest[:]) || runtime.Modules[0].PackageSHA256 != artifact.SHA256 {
		t.Fatalf("模块摘要未绑定已验证制品: %+v", runtime.Modules)
	}
	recovery, err := catalog.ResolveRecoveryRuntime(context.Background(), "tenant-a", 2, spec)
	if err != nil || len(recovery.Modules) != len(spec.Plugins) || recovery.Modules[0].URL != "/v1/portal-recovery-modules/2/1/"+runtime.Modules[0].SHA256+".js" {
		t.Fatalf("恢复模块 URL 未同时绑定 active/fallback revision: %+v %v", recovery.Modules, err)
	}
	asset, err := catalog.ReadFrontendModule(context.Background(), "tenant-a", spec, runtime.Modules[0].SHA256)
	if err != nil || string(asset.Content) != string(module) {
		t.Fatalf("读取已锁定模块失败: asset=%+v err=%v", asset.Descriptor, err)
	}
	if counted.calls != materializationFetches {
		t.Fatalf("运行时热路径不得重新读取制品包: before=%d after=%d", materializationFetches, counted.calls)
	}
	edgeB, err := NewTrustedCatalog([]ArtifactSource{counted}, contentVerifier{}, WithFrontendDeliveryDistribution(originRoot, filepath.Join(deliveryRoot, "edge-b")))
	if err != nil {
		t.Fatal(err)
	}
	beforeColdFill := counted.calls
	if _, err := edgeB.ResolveRuntime(context.Background(), "tenant-a", spec); err != nil || counted.calls != beforeColdFill {
		t.Fatalf("新 Portal Edge 应从中央交付快照冷填充且不读制品包: calls=%d err=%v", counted.calls-beforeColdFill, err)
	}
	if err := edgeB.PrefetchPortal(context.Background(), "tenant-a", spec); err != nil || counted.calls != beforeColdFill {
		t.Fatalf("已就绪 Portal Edge 的后台预取应无副作用: calls=%d err=%v", counted.calls-beforeColdFill, err)
	}
	spec.Resolution.PluginOrigins[ref.ID] = compositioncommonv1.OriginApplication
	if err := catalog.ValidatePortal(context.Background(), "tenant-a", spec); err == nil {
		t.Fatal("应用输入选择 foundation 设计系统必须拒绝")
	}
	spec.Resolution.PluginOrigins[ref.ID] = compositioncommonv1.OriginPlatformProfile
	spec.RenderAdapter.UIContract = "^9.0.0"
	if err := catalog.ValidatePortal(context.Background(), "tenant-a", spec); err == nil {
		t.Fatal("不兼容 UI 契约必须拒绝")
	}
}

func TestFrontendRendererModuleIsDeferredFromTrustedManifest(t *testing.T) {
	manifestRaw := `{"id":"cn.vastplan.foundation.frontend.render.adapter.arco.test","name":"renderer","description":"test","version":"1.0.0","publisher":"vastplan","engines":{"frontend":"^1.0"},"activation":["onPortalStartup"],"entry":{"frontend":"frontend/main.js"},"contributes":{"frontend":{"rendererModules":[{"id":"arco","adapter":"ui.render.adapter","uiContract":"^4.0.0","framework":"arco"}]}}}`
	artifact, packageBytes := packageFrontendFixture(t, manifestRaw, []byte(`export const renderer = {};`))
	manifest, err := pluginv1.ParseManifest([]byte(manifestRaw))
	if err != nil {
		t.Fatal(err)
	}
	asset, err := frontendModule(1, verifiedPortalPlugin{
		ref:          portalapi.PluginRef{ID: artifact.PluginID, Version: artifact.Version, Channel: artifact.Channel},
		artifact:     artifact,
		packageBytes: packageBytes,
		manifest:     manifest,
	})
	if err != nil || !asset.Descriptor.Deferred {
		t.Fatalf("Renderer 模块必须作为延迟前端对象交付: asset=%+v err=%v", asset.Descriptor, err)
	}
}

func TestFrontendShellLibraryModuleIsDeferredFromTrustedManifest(t *testing.T) {
	manifestRaw := `{"id":"cn.vastplan.foundation.frontend.structure.layout.deferred-test","name":"layout","description":"test","version":"1.0.0","publisher":"vastplan","engines":{"frontend":"^1.0"},"activation":["onPortalStartup"],"entry":{"frontend":"frontend/main.js"},"contributes":{"frontend":{"shellLibraries":[{"id":"deferred-test","shell":"ui.structure.shell","uiContract":"^4.0.0"}]}}}`
	artifact, packageBytes := packageFrontendFixture(t, manifestRaw, []byte(`export const shellLibrary = {};`))
	manifest, err := pluginv1.ParseManifest([]byte(manifestRaw))
	if err != nil {
		t.Fatal(err)
	}
	asset, err := frontendModule(1, verifiedPortalPlugin{ref: portalapi.PluginRef{ID: artifact.PluginID, Version: artifact.Version, Channel: artifact.Channel}, artifact: artifact, packageBytes: packageBytes, manifest: manifest})
	if err != nil || !asset.Descriptor.Deferred {
		t.Fatalf("Shell Library 模块必须作为延迟前端对象交付: asset=%+v err=%v", asset.Descriptor, err)
	}
}

func lockTestManagement(spec *portalapi.PortalSpec) {
	profile := spec.Resolution.PlatformProfile
	spec.Resolution.PlatformCatalog = compositioncommonv1.Ref{ID: "catalog", Revision: 1, Digest: strings.Repeat("c", 64)}
	spec.Management = frontendcompositionv1.PortalBinding{
		TenantID: spec.TenantID, PortalID: spec.ID, PlatformProfile: profile,
		Services: []frontendcompositionv1.ManagedService{{ID: "settings", LogicalService: "platform.settings", RoutingDomain: "platform", Capabilities: []frontendcompositionv1.CapabilityGrant{{Capability: "platform.settings", Read: []string{"list"}}}}},
	}
	spec.Resolution.ManagementBindingDigest = compositioncommonv1.Digest(spec.Management)
}

func packageFrontendFixture(t *testing.T, manifest string, module []byte) (pluginv1.Artifact, []byte) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "vastplan.plugin.json"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "frontend"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "frontend", "main.js"), module, 0o600); err != nil {
		t.Fatal(err)
	}
	pkg, _, err := pluginservice.PackageDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := pluginservice.Describe("stable", pkg)
	if err != nil {
		t.Fatal(err)
	}
	return artifact, pkg
}
