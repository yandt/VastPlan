package profileactivation

import (
	"encoding/json"
	"strings"
	"testing"

	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
	deploymentv1 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v1"
	deploymentv2 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v2"
	"cdsoft.com.cn/VastPlan/core/shared/go/platformprofileactivation"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfiguration"
)

func TestBuildProfileCandidatePatchesOnlyIndependentServiceAndUsesGlobalNextRevision(t *testing.T) {
	active := profileTestCatalog(t)
	definition := profileTestDefinition()
	request := profileTestRequest()
	next, previous, err := buildProfileCandidate(active, "tenant-a", "services-a", definition, request)
	if err != nil {
		t.Fatal(err)
	}
	if previous.Revision != 1 || next.Revision != 4 {
		t.Fatalf("候选 revision 必须高于同 ID 全部历史版本: previous=%+v next=%d", previous, next.Revision)
	}
	envelope := next.Services[0].Config["plugins"].(map[string]any)[definition.PluginID].(map[string]any)
	if envelope["region"] != "west" {
		t.Fatalf("候选 Profile 未写入新配置: %#v", next.Services[0].Config)
	}
	original := active.Profiles[0].Services[0].Config["plugins"].(map[string]any)[definition.PluginID].(map[string]any)
	if original["region"] != "east" {
		t.Fatal("构造候选不得原地修改活动 Profile")
	}
	candidateCatalog, err := buildCatalogCandidate(active, "tenant-a", "services-a", next)
	if err != nil {
		t.Fatal(err)
	}
	if candidateCatalog.Revision != active.Revision+1 || len(candidateCatalog.Profiles) != len(active.Profiles)+1 {
		t.Fatalf("候选 Catalog 必须是追加式单调修订: %+v", candidateCatalog)
	}
	_, ref, err := candidateCatalog.Resolve("tenant-a", "services-a")
	if err != nil || ref != profileRef(next) {
		t.Fatalf("目标 binding 未切换到精确候选 Profile: ref=%+v err=%v", ref, err)
	}
}

func TestBuildProfileCandidateRejectsBaselineOrArtifactDrift(t *testing.T) {
	active := profileTestCatalog(t)
	definition := profileTestDefinition()
	request := profileTestRequest()
	active.Profiles[0].Services = []deploymentv2.ServiceUnit{}
	active.Profiles[0].ServiceBaselines = []backendcompositionv1.ServiceBaseline{{ID: "application-security", ServiceClass: "application.backend", Plugins: []deploymentv1.PluginRef{{ID: definition.PluginID, Version: definition.Artifact.Version, Channel: "stable"}}}}
	active.Bindings[0].PlatformProfile = profileRef(active.Profiles[0])
	if _, _, err := buildProfileCandidate(active, "tenant-a", "services-a", definition, request); err == nil {
		t.Fatal("通用配置不得把公共 Service Baseline 冒充为独立 Platform service")
	}
	active = profileTestCatalog(t)
	definition.Artifact.Version = "2.0.0"
	if _, _, err := buildProfileCandidate(active, "tenant-a", "services-a", definition, request); err == nil {
		t.Fatal("配置目录制品身份漂移时必须拒绝")
	}
}

func TestBuildProfileCandidateUpdatesServiceBaselineConfiguration(t *testing.T) {
	active := profileTestCatalog(t)
	definition := profileTestDefinition()
	definition.UnitID = "application-unit"
	definition.ServiceBaselineID = "application-security"
	request := profileTestRequest()
	active.Profiles[0].Services = []deploymentv2.ServiceUnit{}
	active.Profiles[0].ServiceBaselines = []backendcompositionv1.ServiceBaseline{{
		ID: "application-security", ServiceClass: "application.backend",
		Plugins: []deploymentv1.PluginRef{{ID: definition.PluginID, Version: definition.Artifact.Version, Channel: "stable"}},
		Config: map[string]any{
			"plugins":               map[string]any{definition.PluginID: map[string]any{"region": "east"}},
			"environment_allowlist": map[string]any{definition.PluginID: []any{"CONFIG_PATH"}},
		},
	}}
	active.Bindings[0].PlatformProfile = profileRef(active.Profiles[0])
	next, _, err := buildProfileCandidate(active, "tenant-a", "services-a", definition, request)
	if err != nil {
		t.Fatal(err)
	}
	config := next.ServiceBaselines[0].Config
	values := config["plugins"].(map[string]any)[definition.PluginID].(map[string]any)
	if values["region"] != "west" || config["environment_allowlist"] == nil {
		t.Fatalf("公共基线配置没有独立更新或丢失宿主配置: %+v", config)
	}
}

func profileTestCatalog(t *testing.T) backendcompositionv1.BackendPlatformCatalog {
	t.Helper()
	pluginID := "cn.vastplan.platform.example.configurable"
	profile := backendcompositionv1.PlatformProfile{
		Document: compositioncommonv1.Document{Version: 1, Revision: 1, ID: "platform-default"},
		Target:   compositioncommonv1.Target{Kernel: compositioncommonv1.KernelBackend}, ServiceClasses: []string{"application.backend"}, ServiceBaselines: []backendcompositionv1.ServiceBaseline{},
		Services: []deploymentv2.ServiceUnit{{
			ID: "platform-core", Kind: "service", Enabled: true, ServiceRole: "backend", Replicas: 1,
			Plugins: []deploymentv1.PluginRef{{ID: pluginID, Version: "1.0.0", Channel: "stable"}},
			Config:  map[string]any{"plugins": map[string]any{pluginID: map[string]any{"region": "east"}}},
		}},
	}
	var err error
	profile, err = backendcompositionv1.ValidatePlatformProfile(profile)
	if err != nil {
		t.Fatal(err)
	}
	historical := cloneJSON(profile)
	historical.Revision = 3
	catalog := backendcompositionv1.BackendPlatformCatalog{
		Document: compositioncommonv1.Document{Version: 1, Revision: 7, ID: "platform-catalog"},
		Profiles: []backendcompositionv1.PlatformProfile{profile, historical},
		Bindings: []backendcompositionv1.BackendPlatformBinding{{TenantID: "tenant-a", DeploymentName: "services-a", PlatformProfile: profileRef(profile)}},
	}
	validated, err := backendcompositionv1.ValidateBackendPlatformCatalog(catalog)
	if err != nil {
		t.Fatal(err)
	}
	return validated
}

func profileTestDefinition() pluginconfiguration.Definition {
	return pluginconfiguration.Definition{
		ID: "cfg_" + strings.Repeat("a", 24), Deployment: "services-a", UnitID: "platform-core",
		PluginID: "cn.vastplan.platform.example.configurable", PluginName: "Configurable", Origin: deploymentv2.OriginPlatformProfile,
		Artifact: pluginconfiguration.ArtifactIdentity{Version: "1.0.0", Channel: "stable", SHA256: strings.Repeat("b", 64)},
		Scope:    "service", ApplyMode: "restart", ApplyPath: pluginconfiguration.ApplyPlatformProfile,
		Schema: json.RawMessage(`{"type":"object"}`), SchemaDigest: strings.Repeat("c", 64), Values: json.RawMessage(`{"region":"east"}`),
		DeploymentRevision: 4, DeploymentDigest: strings.Repeat("d", 64),
	}
}

func profileTestRequest() platformprofileactivation.PrepareRequest {
	return platformprofileactivation.PrepareRequest{
		CandidateID: "pcfg_" + strings.Repeat("1", 32), ConfigurationID: "cfg_" + strings.Repeat("a", 24),
		ConfigCatalogDigest: strings.Repeat("e", 64), SchemaDigest: strings.Repeat("c", 64), ArtifactSHA256: strings.Repeat("b", 64),
		Values: json.RawMessage(`{"region":"west"}`), DeploymentRevision: 5,
	}
}
