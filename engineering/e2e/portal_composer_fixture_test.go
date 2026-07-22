//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	frontendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/frontend/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/hostfactory"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/nodeagent"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/pluginservice"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/portaltrust"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifacttrust"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	"cdsoft.com.cn/VastPlan/core/shared/go/kernelspi"
	"cdsoft.com.cn/VastPlan/core/shared/go/portalapi"
	"cdsoft.com.cn/VastPlan/core/shared/go/protocolbus"
)

type portalComposerFixture struct {
	deliveryOrigin string
}

type portalFixtureVerifier struct{ verifier nodeagent.ArtifactVerifier }

func (v portalFixtureVerifier) Verify(_ context.Context, ref pluginv1.ArtifactRef, envelope artifacttrust.Envelope) (pluginv1.Artifact, error) {
	verified, err := v.verifier.Verify(ref, envelope)
	if err != nil {
		return pluginv1.Artifact{}, err
	}
	return verified.Artifact(), nil
}

func startPortalComposerFixture(t *testing.T, root string, addressing *portalAddressingFixture) portalComposerFixture {
	t.Helper()
	temporary := t.TempDir()
	repository, err := pluginservice.NewRepository(filepath.Join(temporary, "repository"))
	if err != nil {
		t.Fatal(err)
	}
	publishBuiltPlugin(t, repository,
		"./extensions/plugins/cn.vastplan.foundation.security.portal-access-policy/backend",
		"extensions/plugins/cn.vastplan.foundation.security.portal-access-policy/vastplan.plugin.json")
	publishBuiltPlugin(t, repository,
		"./extensions/plugins/cn.vastplan.platform.configuration.portal-composer/backend",
		"extensions/plugins/cn.vastplan.platform.configuration.portal-composer/vastplan.plugin.json")
	for _, plugin := range portalPlatformBackendPlugins() {
		publishBuiltPlugin(t, repository, plugin.packageDir, plugin.manifest)
	}
	for _, manifest := range portalFoundationFrontendManifests() {
		publishPortalFrontendFixture(t, repository, manifest)
	}

	verifier := nodeagent.NewLocalDevelopmentArtifactVerifier()
	installer := nodeagent.LocalInstaller{Root: filepath.Join(temporary, "installed")}
	policy := installPortalFixturePlugin(t, repository, verifier, installer, pluginv1.ArtifactRef{
		PluginID: "cn.vastplan.foundation.security.portal-access-policy", Version: "0.3.0", Channel: "stable",
	})
	composer := installPortalFixturePlugin(t, repository, verifier, installer, pluginv1.ArtifactRef{
		PluginID: "cn.vastplan.platform.configuration.portal-composer", Version: "1.5.0", Channel: "stable",
	})
	platformCatalog := portalPlatformCatalogForTenant(t, root, "acme")
	config, err := kernelspi.NewMapConfig(map[string]any{
		"platform.portal-composer.stateFile":       filepath.Join(temporary, "composer-state.json"),
		"platform.portal-composer.platformCatalog": string(platformCatalog),
	})
	if err != nil {
		t.Fatal(err)
	}
	deliveryOrigin := filepath.Join(temporary, "frontend-origin")
	catalog, err := portaltrust.NewTrustedCatalog(
		[]portaltrust.ArtifactSource{repository}, portalFixtureVerifier{verifier: verifier}, portaltrust.WithFrontendDeliveryRoot(deliveryOrigin),
	)
	if err != nil {
		t.Fatal(err)
	}
	host, err := hostfactory.NewWithDependencies("0.1.0", t.Logf, kernelspi.Dependencies{Config: config})
	if err != nil {
		t.Fatal(err)
	}
	registerPortalTrustServices(t, host, catalog)
	if err := host.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(host.Stop)
	if _, err := host.LaunchWithPolicy(context.Background(), policy.EntryPath, portalFixtureLaunchPolicy(policy)); err != nil {
		t.Fatalf("启动 Portal 访问策略: %v", err)
	}
	if _, err := host.LaunchWithPolicy(context.Background(), composer.EntryPath, portalFixtureLaunchPolicy(composer)); err != nil {
		t.Fatalf("启动 Portal Composer: %v", err)
	}
	addressing.register(t, func(ctx context.Context, target *contractv1.CallTarget, callContext *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
		response, err := host.Invoke(ctx, target, callContext, payload)
		if err != nil {
			return nil, nil, err
		}
		if response == nil || response.Result == nil {
			return nil, nil, errors.New("Portal Composer 响应为空")
		}
		return response.Result, response.Payload, nil
	})
	return portalComposerFixture{deliveryOrigin: deliveryOrigin}
}

func registerPortalTrustServices(t *testing.T, host *protocolbus.Host, catalog *portaltrust.TrustedCatalog) {
	t.Helper()
	services := map[string]protocolbus.HostService{
		portalapi.KernelCatalogValidationCapability:            portaltrust.CatalogValidationService(catalog),
		portalapi.KernelCatalogMaterializationCapability:       portaltrust.CatalogMaterializationService(catalog),
		portalapi.KernelArtifactReferencePublicationCapability: portaltrust.ArtifactReferencePublicationService(portaltrust.DevelopmentArtifactReferencePublisher{}),
		portalapi.KernelTestArtifactValidationCapability:       portaltrust.CatalogTestArtifactValidationService(catalog),
	}
	for capability, service := range services {
		if err := host.RegisterHostService(extpoint.KernelService, capability, service); err != nil {
			t.Fatal(err)
		}
	}
}

func installPortalFixturePlugin(t *testing.T, repository *pluginservice.Repository, verifier nodeagent.ArtifactVerifier, installer nodeagent.LocalInstaller, ref pluginv1.ArtifactRef) nodeagent.InstalledPlugin {
	t.Helper()
	envelope, err := repository.Fetch(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	verified, err := verifier.Verify(ref, envelope)
	if err != nil {
		t.Fatal(err)
	}
	installed, err := installer.Install(verified)
	if err != nil {
		t.Fatal(err)
	}
	return installed
}

func portalFixtureLaunchPolicy(installed nodeagent.InstalledPlugin) protocolbus.LaunchPolicy {
	return protocolbus.LaunchPolicy{
		PluginID: installed.ID, Publisher: installed.Publisher, Version: installed.Version,
		Contributions: installed.Contract.Contributions, KernelServices: installed.Contract.KernelServices, ContextAccess: installed.Contract.ContextAccess,
	}
}

func portalPlatformCatalogForTenant(t *testing.T, root, tenantID string) []byte {
	t.Helper()
	catalog, err := frontendcompositionv1.ParsePortalPlatformCatalogFile(filepath.Join(root, "engineering", "deploy", "portal-platform-catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	for index := range catalog.Bindings {
		catalog.Bindings[index].TenantID = tenantID
	}
	raw, err := json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func publishPortalFrontendFixture(t *testing.T, repository *pluginservice.Repository, manifestPath string) {
	t.Helper()
	manifestRaw, err := os.ReadFile(filepath.Join(repoRoot(t), manifestPath))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := pluginv1.ParseManifest(manifestRaw)
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "vastplan.plugin.json"), manifestRaw, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, entry := range manifest.Entry {
		if entry == "" {
			continue
		}
		filename := filepath.Join(directory, filepath.FromSlash(entry))
		if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
			t.Fatal(err)
		}
		content := []byte(`export default { register() {} };`)
		if err := os.WriteFile(filename, content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	copyPortalFixtureNotice(t, directory, manifest.LicenseFile)
	copyPortalFixtureNotice(t, directory, manifest.NoticeFile)
	packageBytes, _, err := pluginservice.PackageDirectory(directory)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Publish("stable", packageBytes); err != nil {
		t.Fatal(err)
	}
}

func copyPortalFixtureNotice(t *testing.T, directory, name string) {
	t.Helper()
	if name == "" {
		return
	}
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), filepath.FromSlash(name)))
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(directory, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

func portalPlatformBackendPlugins() []struct{ packageDir, manifest string } {
	return []struct{ packageDir, manifest string }{
		{"./extensions/plugins/cn.vastplan.platform.configuration.global-settings/backend", "extensions/plugins/cn.vastplan.platform.configuration.global-settings/vastplan.plugin.json"},
		{"./extensions/plugins/cn.vastplan.platform.security.credentials/backend", "extensions/plugins/cn.vastplan.platform.security.credentials/vastplan.plugin.json"},
		{"./extensions/plugins/cn.vastplan.platform.data.relational.connection-manager/backend", "extensions/plugins/cn.vastplan.platform.data.relational.connection-manager/vastplan.plugin.json"},
		{"./extensions/plugins/cn.vastplan.platform.artifacts.repository/backend", "extensions/plugins/cn.vastplan.platform.artifacts.repository/vastplan.plugin.json"},
		{"./extensions/plugins/cn.vastplan.platform.infrastructure.deployment-manager/backend", "extensions/plugins/cn.vastplan.platform.infrastructure.deployment-manager/vastplan.plugin.json"},
	}
}

func portalFoundationFrontendManifests() []string {
	return []string{
		"extensions/plugins/cn.vastplan.foundation.frontend.runtime.engine.react/vastplan.plugin.json",
		"extensions/plugins/cn.vastplan.foundation.frontend.render.adapter/vastplan.plugin.json",
		"extensions/plugins/cn.vastplan.foundation.frontend.render.adapter.arco/vastplan.plugin.json",
		"extensions/plugins/cn.vastplan.foundation.frontend.render.adapter.mui/vastplan.plugin.json",
		"extensions/plugins/cn.vastplan.foundation.frontend.structure.shell/vastplan.plugin.json",
		"extensions/plugins/cn.vastplan.foundation.frontend.structure.layout.standard/vastplan.plugin.json",
		"extensions/plugins/cn.vastplan.foundation.frontend.structure.layout.top-navigation/vastplan.plugin.json",
		"extensions/plugins/cn.vastplan.foundation.frontend.workflow.workbench/vastplan.plugin.json",
	}
}
