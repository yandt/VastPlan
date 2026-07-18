package edge

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cdsoft.com.cn/VastPlan/core/kernels/backend/pluginservice"
	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifacttrust"
	"cdsoft.com.cn/VastPlan/core/shared/go/portalapi"
)

type catalogSource map[string]artifacttrust.Envelope

func (s catalogSource) Fetch(_ context.Context, ref pluginv1.ArtifactRef) (artifacttrust.Envelope, error) {
	return s[ref.PluginID+"@"+ref.Version], nil
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
	manifest := `{"id":"com.vastplan.foundation.frontend.design-system.test","name":"test","description":"test","version":"1.0.0","publisher":"vastplan","engines":{"frontend":"^1.0"},"activation":["onPortalStartup"],"entry":{"frontend":"frontend/main.js"},"contributes":{"frontend":{"designSystems":[{"id":"ui.design-system","uiContract":"^1.0.0","framework":"test","capabilities":["layout","menu","overlay","form","data","feedback","theme"]}]}}}`
	if err := os.WriteFile(filepath.Join(dir, "vastplan.plugin.json"), []byte(manifest), 0o600); err != nil {
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
	catalog, err := NewTrustedCatalog([]ArtifactSource{source}, contentVerifier{})
	if err != nil {
		t.Fatal(err)
	}
	ref := portalapi.PluginRef{ID: artifact.PluginID, Version: artifact.Version}
	spec := portalapi.PortalSpec{Revision: 1, ID: "admin", TenantID: "tenant-a", Route: "/", DesignSystem: portalapi.DesignSystem{PluginRef: ref, UIContract: "^1.0.0"}, Plugins: []portalapi.PluginRef{ref}, Resolution: portalapi.Resolution{PlatformProfile: compositioncommonv1.Ref{ID: "default", Revision: 1, Digest: strings.Repeat("a", 64)}, ApplicationComposition: compositioncommonv1.Ref{ID: "admin", Revision: 1, Digest: strings.Repeat("b", 64)}, PluginOrigins: map[string]string{ref.ID: compositioncommonv1.OriginPlatformProfile}}}
	if err := catalog.ValidatePortal(context.Background(), "tenant-a", spec); err != nil {
		t.Fatalf("有效且已验证的设计系统应通过: %v", err)
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
