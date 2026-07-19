package edge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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

func (s *countingCatalogSource) Fetch(ctx context.Context, ref pluginv1.ArtifactRef) (artifacttrust.Envelope, error) {
	s.calls++
	return s.catalogSource.Fetch(ctx, ref)
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

func TestTrustedCatalogRequiresVerifiedFrontendDesignSystemContribution(t *testing.T) {
	dir := t.TempDir()
	module := []byte(`export default { id: "ui.design-system" };`)
	manifest := `{"id":"com.vastplan.foundation.frontend.design-system.test","name":"test","description":"test","version":"1.0.0","publisher":"vastplan","engines":{"frontend":"^1.0"},"activation":["onPortalStartup"],"entry":{"frontend":"frontend/main.js"},"contributes":{"frontend":{"designSystems":[{"id":"ui.design-system","uiContract":"^2.0.0","framework":"test","capabilities":["layout","menu","overlay","form","data","feedback","theme"]}]}}}`
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
	compositionArtifact, compositionPackage := packageFrontendFixture(t, `{"id":"com.vastplan.foundation.frontend.composition.test","name":"composition","description":"test","version":"1.0.0","publisher":"vastplan","engines":{"frontend":"^1.0"},"activation":["onPortalStartup"],"entry":{"frontend":"frontend/main.js"},"contributes":{"frontend":{"shellCompositions":[{"id":"ui.shell-composition","uiContract":"^2.0.0"}]}}}`, []byte(`export default { id: "ui.shell-composition" };`))
	layoutArtifact, layoutPackage := packageFrontendFixture(t, `{"id":"com.vastplan.foundation.frontend.layout.test","name":"layout","description":"test","version":"1.0.0","publisher":"vastplan","engines":{"frontend":"^1.0"},"activation":["onPortalStartup"],"entry":{"frontend":"frontend/main.js"},"contributes":{"frontend":{"shellLayouts":[{"id":"ui.shell-layout","uiContract":"^2.0.0"}]}}}`, []byte(`export default { id: "ui.shell-layout" };`))
	source[compositionArtifact.PluginID+"@"+compositionArtifact.Version] = artifacttrust.Envelope{Artifact: compositionArtifact, PackageBytes: compositionPackage}
	source[layoutArtifact.PluginID+"@"+layoutArtifact.Version] = artifacttrust.Envelope{Artifact: layoutArtifact, PackageBytes: layoutPackage}
	counted := &countingCatalogSource{catalogSource: source}
	deliveryRoot := t.TempDir()
	originRoot := filepath.Join(deliveryRoot, "origin")
	catalog, err := NewTrustedCatalog([]ArtifactSource{counted}, contentVerifier{}, WithFrontendDeliveryDistribution(originRoot, filepath.Join(deliveryRoot, "edge-a")))
	if err != nil {
		t.Fatal(err)
	}
	ref := portalapi.PluginRef{ID: artifact.PluginID, Version: artifact.Version}
	compositionRef := portalapi.PluginRef{ID: compositionArtifact.PluginID, Version: compositionArtifact.Version}
	layoutRef := portalapi.PluginRef{ID: layoutArtifact.PluginID, Version: layoutArtifact.Version}
	spec := portalapi.PortalSpec{Revision: 1, ID: "admin", TenantID: "tenant-a", Route: "/", DesignSystem: portalapi.DesignSystem{PluginRef: ref, UIContract: "^2.0.0"}, Composition: portalapi.ShellComposition{PluginRef: compositionRef, UIContract: "^2.0.0"}, Layout: portalapi.ShellLayout{PluginRef: layoutRef, UIContract: "^2.0.0"}, Plugins: []portalapi.PluginRef{ref, compositionRef, layoutRef}, Resolution: portalapi.Resolution{PlatformProfile: compositioncommonv1.Ref{ID: "default", Revision: 1, Digest: strings.Repeat("a", 64)}, ApplicationComposition: compositioncommonv1.Ref{ID: "admin", Revision: 1, Digest: strings.Repeat("b", 64)}, PluginOrigins: map[string]string{ref.ID: compositioncommonv1.OriginPlatformProfile, compositionRef.ID: compositioncommonv1.OriginPlatformProfile, layoutRef.ID: compositioncommonv1.OriginPlatformProfile}}}
	lockTestManagement(&spec)
	if err := catalog.ValidatePortal(context.Background(), "tenant-a", spec); err != nil {
		t.Fatalf("有效且已验证的设计系统应通过: %v", err)
	}
	beforeMaterialization := counted.calls
	if err := catalog.MaterializePortal(context.Background(), "tenant-a", spec); err != nil {
		t.Fatal(err)
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
	if len(runtime.Modules) != 3 || runtime.Modules[0].SHA256 != hex.EncodeToString(wantDigest[:]) || runtime.Modules[0].PackageSHA256 != artifact.SHA256 {
		t.Fatalf("模块摘要未绑定已验证制品: %+v", runtime.Modules)
	}
	recovery, err := catalog.ResolveRecoveryRuntime(context.Background(), "tenant-a", 2, spec)
	if err != nil || len(recovery.Modules) != 3 || recovery.Modules[0].URL != "/v1/portal-recovery-modules/2/1/"+runtime.Modules[0].SHA256+".js" {
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
	spec.DesignSystem.UIContract = "^2.0.0"
	if err := catalog.ValidatePortal(context.Background(), "tenant-a", spec); err == nil {
		t.Fatal("不兼容 UI 契约必须拒绝")
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
