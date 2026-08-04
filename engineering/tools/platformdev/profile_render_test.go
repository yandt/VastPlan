package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	artifactrepositoryv1 "cdsoft.com.cn/VastPlan/contracts/schemas/artifactrepository/v1"
	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
	frontendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/frontend/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/configfile"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/servicemodel"
)

func TestSeedPlatformServicePoliciesMatchSignedManifests(t *testing.T) {
	root := repositoryRoot(t)
	profile, err := backendcompositionv1.ParsePlatformProfileFile(filepath.Join(root, "engineering", "deploy", "platform-management-profile.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, unit := range profile.Services {
		unitPolicy := servicemodel.Policy{InstancePolicy: unit.InstancePolicy, StateModel: unit.StateModel, Visibility: unit.Visibility, Routing: unit.Routing, RoutingDomain: unit.RoutingDomain}
		for _, ref := range unit.Plugins {
			raw, readErr := os.ReadFile(filepath.Join(root, "extensions", "plugins", ref.ID, "vastplan.plugin.json"))
			if readErr != nil {
				t.Fatalf("读取 %s 签名清单: %v", ref.ID, readErr)
			}
			manifest, parseErr := pluginv1.ParseManifest(raw)
			if parseErr != nil {
				t.Fatalf("解析 %s 签名清单: %v", ref.ID, parseErr)
			}
			contributions, contributionErr := pluginv1.BackendRuntimeContributions(manifest)
			if contributionErr != nil {
				t.Fatalf("解析 %s runtime contribution: %v", ref.ID, contributionErr)
			}
			for _, contribution := range contributions {
				contributionPolicy := servicemodel.Policy{InstancePolicy: contribution.InstancePolicy, StateModel: contribution.StateModel, Visibility: contribution.Visibility, Routing: contribution.Routing, RoutingDomain: contribution.RoutingDomain}
				if !servicemodel.Equal(unitPolicy, contributionPolicy) && !pluginv1.IsLocalPermissionAuxiliary(contribution) {
					t.Fatalf("Seed unit %s 策略与 %s/%s 签名清单不一致: unit=%+v contribution=%+v", unit.ID, ref.ID, contribution.ID, unitPolicy, contributionPolicy)
				}
			}
		}
	}
}

func TestRenderPlatformProfileProducesValidProviderComposition(t *testing.T) {
	template, err := os.ReadFile(filepath.Join("..", "..", "deploy", "platform-management-profile.json"))
	if err != nil {
		t.Fatal(err)
	}
	portalCatalog, err := os.ReadFile(filepath.Join("..", "..", "deploy", "portal-platform-catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	repositoryProfile := artifactrepositoryv1.Profile{
		Version: 1, ID: "local-testing", Protocol: artifactrepositoryv1.ProtocolLocalTest,
		Endpoint: "unix:///private/tmp/vastplan-state/repository.sock", Channels: []string{"testing"}, DevelopmentOnly: true,
	}
	raw, err := renderPlatformProfile(template, portalCatalog, "/private/tmp/vastplan-dev", "/private/tmp/vastplan-state", "127.0.0.1:9443", repositoryProfile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "__VASTPLAN_") {
		t.Fatal("渲染后的平台 Profile 不得保留占位符")
	}
	profile, err := backendcompositionv1.ParsePlatformProfile(raw)
	if err != nil {
		t.Fatalf("渲染后的平台 Profile 无效: %v", err)
	}
	for _, service := range profile.Services {
		if service.ID == "platform-database-runtime" && service.Replicas != 1 {
			t.Fatalf("开发 Profile 必须把单节点 Database Runtime 缩放为 1: %#v", service)
		}
		if service.ID == "platform-api-exposure" {
			plugins := service.Config["plugins"].(map[string]any)
			exposure := plugins["cn.vastplan.platform.integration.api-exposure"].(map[string]any)
			if exposure["contractCatalogFile"] != "/private/tmp/vastplan-state/api-contract-catalog.json" {
				t.Fatalf("API Contract Catalog 必须使用跨重启稳定路径: %#v", exposure)
			}
		}
		if service.ID != "platform-artifacts" {
			continue
		}
		plugins := service.Config["plugins"].(map[string]any)
		repository := plugins["cn.vastplan.platform.artifacts.repository"].(map[string]any)
		profile := repository["repositoryProfile"].(map[string]any)
		if profile["protocol"] != artifactrepositoryv1.ProtocolLocalTest || profile["endpoint"] != repositoryProfile.Endpoint || repository["storageProvider"] != "platform.artifacts.storage.file" {
			t.Fatalf("制品仓库插件配置未正确渲染: %#v", repository)
		}
		return
	}
	t.Fatal("平台 Profile 缺少 platform-artifacts service")
}

func TestPlatformManagementDeploymentCountsEnabledServices(t *testing.T) {
	runDir := t.TempDir()
	raw := []byte(`{
  "version": 1,
  "revision": 12,
  "id": "test-profile",
  "target": {"kernel": "backend"},
  "serviceClasses": ["application.backend"],
  "serviceBaselines": [],
  "services": [
    {"id":"enabled","kind":"service","enabled":true,"service_role":"backend","logical_service":"test.enabled","instance_policy":"per-kernel","state_model":"local-ephemeral","visibility":"local","routing":"direct","replicas":1,"plugins":[{"id":"cn.vastplan.test.enabled","version":"1.0.0","channel":"stable"}]},
    {"id":"disabled","kind":"service","enabled":false,"service_role":"backend","logical_service":"test.disabled","instance_policy":"per-kernel","state_model":"local-ephemeral","visibility":"local","routing":"direct","replicas":1,"plugins":[{"id":"cn.vastplan.test.disabled","version":"1.0.0","channel":"stable"}]}
  ]
}`)
	if err := os.WriteFile(filepath.Join(runDir, "platform-management-profile.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	runtime := runtime{runDir: runDir}
	revision, units, err := runtime.platformManagementDeployment()
	if err != nil {
		t.Fatal(err)
	}
	if revision != "12" || units != 1 {
		t.Fatalf("部署摘要错误: revision=%s units=%d", revision, units)
	}
}

func TestPortalPlatformBindingUsesCurrentProfileDigest(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "deploy", "portal-platform-catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	var catalog frontendcompositionv1.PortalPlatformCatalog
	if err := json.Unmarshal(raw, &catalog); err != nil {
		t.Fatal(err)
	}
	if len(catalog.Profiles) != 1 || len(catalog.Bindings) != 1 {
		t.Fatalf("开发 Portal Catalog 必须包含唯一 Profile 与 Binding: profiles=%d bindings=%d", len(catalog.Profiles), len(catalog.Bindings))
	}
	profile, binding := catalog.Profiles[0], catalog.Bindings[0]
	digest := profile.Digest()
	if binding.PlatformProfile.ID != profile.ID || binding.PlatformProfile.Revision != profile.Revision || binding.PlatformProfile.Digest != digest {
		t.Fatalf("Portal Binding 未引用当前 Profile: want=%s@%d/%s got=%s@%d/%s", profile.ID, profile.Revision, digest, binding.PlatformProfile.ID, binding.PlatformProfile.Revision, binding.PlatformProfile.Digest)
	}
	if _, err := frontendcompositionv1.ValidatePortalPlatformCatalog(catalog); err != nil {
		t.Fatalf("开发 Portal Catalog 无效: %v", err)
	}
}

func TestCompilePortalPlatformCatalogDerivesBrowserOperationsFromSignedManifests(t *testing.T) {
	root := repositoryRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "engineering", "deploy", "portal-platform-catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	runtime := runtime{options: options{root: root}}
	compiledRaw, err := runtime.compilePortalPlatformCatalog(raw)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := frontendcompositionv1.ParsePortalPlatformCatalog(compiledRaw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := frontendcompositionv1.ValidateResolvedPortalPlatformCatalog(compiled); err != nil {
		t.Fatalf("已编译 Catalog 必须具备精确浏览器 operation snapshot: %v", err)
	}
	for _, service := range compiled.Bindings[0].Services {
		if service.ID != "database" {
			continue
		}
		grant := service.Capabilities[0]
		if !containsString(grant.Read, "describe") || !containsString(grant.Write, "test") {
			t.Fatalf("数据库浏览器操作必须由 Manifest 默认派生: %+v", grant)
		}
		return
	}
	t.Fatal("已编译 Catalog 缺少数据库服务")
}

func TestBrowserExposedPluginReleaseMetadataStaysSynchronized(t *testing.T) {
	root := repositoryRoot(t)
	cases := []struct {
		pluginID      string
		backendSource string
	}{
		{"cn.vastplan.platform.configuration.global-settings", "settings/service.go"},
		{"cn.vastplan.platform.configuration.plugin-settings", "pluginsettings/service.go"},
		{"cn.vastplan.platform.security.credentials", "credentials/vault_transit.go"},
		{"cn.vastplan.platform.data.relational.connection-manager", "backend/main.go"},
		{"cn.vastplan.platform.artifacts.repository", "backend/main.go"},
		{"cn.vastplan.platform.integration.api-exposure", "apiexposure/types.go"},
		{"cn.vastplan.platform.infrastructure.deployment-manager", "deploymentmanager/service.go"},
		{"cn.vastplan.platform.artifacts.marketplace", "marketplace/config.go"},
		{"cn.vastplan.platform.security.authorization-policy", "authorizationpolicy/model.go"},
	}
	for _, test := range cases {
		t.Run(test.pluginID, func(t *testing.T) {
			pluginRoot := filepath.Join(root, "extensions", "plugins", test.pluginID)
			manifestRaw, err := os.ReadFile(filepath.Join(pluginRoot, "vastplan.plugin.json"))
			if err != nil {
				t.Fatal(err)
			}
			manifest, err := pluginv1.ParseManifest(manifestRaw)
			if err != nil || manifest.Authorization == nil || !manifest.Authorization.BrowserExposed {
				t.Fatalf("浏览器暴露插件必须拥有有效且显式的 Manifest 声明: manifest=%+v err=%v", manifest.Authorization, err)
			}
			backendRaw, err := os.ReadFile(filepath.Join(pluginRoot, test.backendSource))
			if err != nil || !strings.Contains(string(backendRaw), manifest.Version) {
				t.Fatalf("Backend runtime identity 未同步 Manifest %s: %v", manifest.Version, err)
			}
			frontendRaw, err := os.ReadFile(filepath.Join(pluginRoot, "frontend", "package.json"))
			if err != nil {
				t.Fatal(err)
			}
			var frontendPackage struct {
				Version string `json:"version"`
			}
			if err := json.Unmarshal(frontendRaw, &frontendPackage); err != nil || frontendPackage.Version != manifest.Version {
				t.Fatalf("Frontend package version 未同步 Manifest: want=%s got=%s err=%v", manifest.Version, frontendPackage.Version, err)
			}
		})
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestPortalManagementAPIReferenceMatchesVerifiedManifestContract(t *testing.T) {
	manifestRaw, err := os.ReadFile(filepath.Join("..", "..", "..", "extensions", "plugins", "cn.vastplan.platform.integration.api-exposure", "vastplan.plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := pluginv1.ParseManifest(manifestRaw)
	if err != nil {
		t.Fatal(err)
	}
	contractCatalog, err := pluginv1.BuildAPIContractCatalog(1, []pluginv1.APIContractCatalogSource{{Manifest: manifest, ArtifactSHA256: strings.Repeat("a", 64)}})
	if err != nil || len(contractCatalog.Contracts) != 1 {
		t.Fatalf("API Contract Catalog 无效: %+v err=%v", contractCatalog, err)
	}
	portalRaw, err := os.ReadFile(filepath.Join("..", "..", "deploy", "portal-platform-catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	portal, err := frontendcompositionv1.ParsePortalPlatformCatalog(portalRaw)
	if err != nil {
		t.Fatal(err)
	}
	var reference *frontendcompositionv1.ManagementAPI
	for _, service := range portal.Bindings[0].Services {
		if service.ID == "api-exposure" && len(service.APIs) == 1 {
			reference = &service.APIs[0]
		}
	}
	if reference == nil {
		t.Fatal("API Exposure 服务必须绑定唯一 Management API")
	}
	trusted := contractCatalog.Contracts[0].Reference
	if reference.ContractID != trusted.ContractID || reference.ContractVersion != trusted.ContractVersion || reference.ContractDigest != trusted.ContractDigest {
		t.Fatalf("Portal Management API 引用未锁定当前可信 Contract: portal=%+v trusted=%+v", *reference, trusted)
	}
}

func TestWriteSeedRepositoryProfileUsesPrivateRunPaths(t *testing.T) {
	runDir := t.TempDir()
	runtime := runtime{runDir: runDir, options: options{seedArtifactListen: "127.0.0.1:18442"}}
	if err := runtime.writeSeedRepositoryProfile(); err != nil {
		t.Fatal(err)
	}
	raw, err := configfile.Load(filepath.Join(runDir, "seed-repository.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var profile map[string]any
	if err := json.Unmarshal(raw, &profile); err != nil {
		t.Fatal(err)
	}
	if profile["listen"] != "127.0.0.1:18442" || profile["repositoryRoot"] != filepath.Join(runDir, "repository") {
		t.Fatalf("Seed Profile 路径或监听地址错误: %s", raw)
	}
	if profile["trustFile"] != filepath.Join(runDir, "secrets", "seed-artifact-trust.json") {
		t.Fatalf("Seed Profile 只能加载 Seed-only 信任文档: %s", raw)
	}
	info, err := os.Stat(filepath.Join(runDir, "seed-repository.yaml"))
	if err != nil || info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("Seed Profile 必须仅属主可访问: info=%v err=%v", info, err)
	}
}
