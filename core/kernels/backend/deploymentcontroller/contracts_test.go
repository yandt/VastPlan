package deploymentcontroller

import (
	"errors"
	"strings"
	"testing"

	deploymentv1 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v1"
	deploymentv2 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v2"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

type contractArtifactReader map[string]pluginv1.Artifact

func (r contractArtifactReader) Read(ref pluginv1.ArtifactRef) (pluginv1.Artifact, []byte, error) {
	artifact, ok := r[ref.PluginID+"@"+ref.Version]
	if !ok {
		return pluginv1.Artifact{}, nil, errors.New("not found")
	}
	return artifact, nil, nil
}

func TestValidateDeploymentContractsBuildsRemoteDAGAndChecksVersion(t *testing.T) {
	databaseManifest := []byte(`{
		"id":"com.example.database","name":"database","description":"database",
		"version":"1.2.0","publisher":"example","engines":{"backend":"^1.0"},
		"runtime":{"instancePolicy":"active-active","stateModel":"external-shared","visibility":"cluster","routing":"queue"},
		"activation":["onStartup"],"entry":{"backend":"backend/main"},
		"contributes":{"backend":{"tools":[{"id":"platform.database","service_role":"backend","title":"database","subcommands":[]}]}}
	}`)
	consumerManifest := []byte(`{
		"id":"com.example.consumer","name":"consumer","description":"consumer",
		"version":"1.0.0","publisher":"example","engines":{"backend":"^1.0"},
		"runtime":{"instancePolicy":"active-active","stateModel":"external-shared","visibility":"cluster","routing":"queue",
			"requires":[{"capability":"platform.database","version":"^1.0.0","scope":"remote","kind":"strong","ready":"readiness","failurePolicy":"retry","logicalService":"platform.database"}]},
		"activation":["onStartup"],"entry":{"backend":"backend/main"},
		"contributes":{"backend":{"tools":[{"id":"consumer.tool","service_role":"backend","title":"consumer","subcommands":[]}]}}
	}`)
	reader := contractArtifactReader{
		"com.example.database@1.2.0": {PluginID: "com.example.database", Version: "1.2.0", Channel: "stable", Manifest: databaseManifest},
		"com.example.consumer@1.0.0": {PluginID: "com.example.consumer", Version: "1.0.0", Channel: "stable", Manifest: consumerManifest},
	}
	deployment := deploymentv2.Deployment{Version: 2, Revision: 1, Metadata: deploymentv1.Metadata{Name: "prod"}, Units: []deploymentv2.ServiceUnit{
		{ID: "database", Kind: "service", Enabled: true, ServiceRole: "backend", LogicalService: "platform.database", InstancePolicy: "active-active", StateModel: "external-shared", Visibility: "cluster", Routing: "queue", Replicas: 1, Plugins: []deploymentv1.PluginRef{{ID: "com.example.database", Version: "1.2.0", Channel: "stable"}}},
		{ID: "api", Kind: "service", Enabled: true, ServiceRole: "backend", LogicalService: "business.api", InstancePolicy: "active-active", StateModel: "external-shared", Visibility: "cluster", Routing: "queue", Replicas: 1, Plugins: []deploymentv1.PluginRef{{ID: "com.example.consumer", Version: "1.0.0", Channel: "stable"}}},
	}}
	deployment.Resolution.PluginOrigins = map[string]string{
		"com.example.database": deploymentv2.OriginApplication,
		"com.example.consumer": deploymentv2.OriginApplication,
	}
	graph := map[string][]string{"database": nil, "api": nil}
	if err := validateDeploymentContracts(deployment, graph, reader); err != nil {
		t.Fatal(err)
	}
	if len(graph["api"]) != 1 || graph["api"][0] != "database" {
		t.Fatalf("远端强依赖必须进入全局 DAG: %+v", graph)
	}

	bad := strings.ReplaceAll(string(consumerManifest), `"^1.0.0"`, `"^2.0.0"`)
	reader["com.example.consumer@1.0.0"] = pluginv1.Artifact{PluginID: "com.example.consumer", Version: "1.0.0", Channel: "stable", Manifest: []byte(bad)}
	if err := validateDeploymentContracts(deployment, map[string][]string{"database": nil, "api": nil}, reader); err == nil || !strings.Contains(err.Error(), "版本范围") {
		t.Fatalf("版本冲突必须在发布 assignment 前报告: %v", err)
	}
}

func TestValidateDeploymentContractsDefersExplicitExternalRemoteDependencyToReadiness(t *testing.T) {
	consumerManifest := []byte(`{
		"id":"com.example.consumer","name":"consumer","description":"consumer",
		"version":"1.0.0","publisher":"example","engines":{"backend":"^1.0"},
		"runtime":{"instancePolicy":"active-active","stateModel":"external-shared","visibility":"cluster","routing":"queue","routingDomain":"application",
			"requires":[{"capability":"platform.settings","scope":"remote","kind":"strong","ready":"readiness","failurePolicy":"fail","logicalService":"platform.settings","routingDomain":"platform"}]},
		"activation":["onStartup"],"entry":{"backend":"backend/main"},
		"contributes":{"backend":{"tools":[{"id":"consumer.tool","service_role":"backend","title":"consumer","subcommands":[]}]}}
	}`)
	reader := contractArtifactReader{"com.example.consumer@1.0.0": {
		PluginID: "com.example.consumer", Version: "1.0.0", Channel: "stable", Manifest: consumerManifest,
	}}
	deployment := deploymentv2.Deployment{
		Version: 2, Revision: 1, Metadata: deploymentv1.Metadata{Name: "application"},
		Resolution: deploymentv2.Resolution{PluginOrigins: map[string]string{"com.example.consumer": deploymentv2.OriginApplication}},
		Units: []deploymentv2.ServiceUnit{{
			ID: "api", Kind: "service", Enabled: true, ServiceRole: "backend", LogicalService: "application.api",
			InstancePolicy: "active-active", StateModel: "external-shared", Visibility: "cluster", Routing: "queue", RoutingDomain: "application", Replicas: 1,
			Plugins: []deploymentv1.PluginRef{{ID: "com.example.consumer", Version: "1.0.0", Channel: "stable"}},
		}},
	}
	if err := validateDeploymentContracts(deployment, map[string][]string{"api": nil}, reader); err != nil {
		t.Fatalf("跨 Deployment 的精确 logical service 依赖应交给全局 readiness gate: %v", err)
	}
}

func TestValidateDeploymentContractsRejectsForgedApplicationOrigin(t *testing.T) {
	manifest := []byte(`{
		"id":"cn.vastplan.foundation.security.policy","name":"policy","description":"policy",
		"version":"1.0.0","publisher":"vastplan","engines":{"backend":"^1.0"},
		"activation":["onStartup"],"entry":{"backend":"backend/main"},
		"contributes":{"backend":{"tools":[{"id":"platform.policy","service_role":"backend","title":"policy","subcommands":[]}]}}
	}`)
	reader := contractArtifactReader{"cn.vastplan.foundation.security.policy@1.0.0": {
		PluginID: "cn.vastplan.foundation.security.policy", Version: "1.0.0", Channel: "stable", Manifest: manifest,
	}}
	deployment := deploymentv2.Deployment{
		Version: 2, Revision: 1, Metadata: deploymentv1.Metadata{Name: "prod"},
		Resolution: deploymentv2.Resolution{PluginOrigins: map[string]string{"cn.vastplan.foundation.security.policy": deploymentv2.OriginApplication}},
		Units:      []deploymentv2.ServiceUnit{{ID: "api", Kind: "service", Enabled: true, ServiceRole: "backend", Replicas: 1, Plugins: []deploymentv1.PluginRef{{ID: "cn.vastplan.foundation.security.policy", Version: "1.0.0", Channel: "stable"}}}},
	}
	if err := validateDeploymentContracts(deployment, map[string][]string{"api": nil}, reader); err == nil || !strings.Contains(err.Error(), "应用来源") {
		t.Fatalf("Controller 必须拒绝伪造的平台插件应用来源: %v", err)
	}
}

func TestValidateDeploymentContractsAllowsLocalPermissionAuxiliary(t *testing.T) {
	serviceManifest := []byte(`{
		"id":"cn.vastplan.platform.configuration.settings","name":"settings","description":"settings","version":"1.0.0","publisher":"vastplan","engines":{"backend":"^1.0"},
		"runtime":{"instancePolicy":"leader","stateModel":"leader-owned","visibility":"cluster","routing":"leader","routingDomain":"platform"},
		"activation":["onStartup"],"entry":{"backend":"backend/main"},
		"contributes":{"backend":{"tools":[{"id":"platform.settings","service_role":"backend","title":"settings","subcommands":[]}]}}
	}`)
	policyManifest := []byte(`{
		"id":"cn.vastplan.foundation.security.platform-admin-access-policy","name":"policy","description":"policy","version":"1.0.0","publisher":"vastplan","engines":{"backend":"^1.0"},
		"runtime":{"instancePolicy":"per-kernel","stateModel":"local-ephemeral","visibility":"local","routing":"direct"},
		"activation":["onStartup"],"entry":{"backend":"backend/main"},
		"contributes":{"backend":{"permissionCheckers":[{"id":"platform.admin","service_role":"backend","title":"policy","priority":1000,"applies":{}}]}}
	}`)
	reader := contractArtifactReader{
		"cn.vastplan.platform.configuration.settings@1.0.0":                  {PluginID: "cn.vastplan.platform.configuration.settings", Version: "1.0.0", Channel: "stable", Manifest: serviceManifest},
		"cn.vastplan.foundation.security.platform-admin-access-policy@1.0.0": {PluginID: "cn.vastplan.foundation.security.platform-admin-access-policy", Version: "1.0.0", Channel: "stable", Manifest: policyManifest},
	}
	deployment := deploymentv2.Deployment{
		Version: 2, Revision: 1, Metadata: deploymentv1.Metadata{Name: "platform"},
		Resolution: deploymentv2.Resolution{PluginOrigins: map[string]string{
			"cn.vastplan.platform.configuration.settings":                  deploymentv2.OriginPlatformProfile,
			"cn.vastplan.foundation.security.platform-admin-access-policy": deploymentv2.OriginPlatformProfile,
		}},
		Units: []deploymentv2.ServiceUnit{{
			ID: "settings", Kind: "service", Enabled: true, ServiceRole: "backend", LogicalService: "platform.settings",
			InstancePolicy: "leader", StateModel: "leader-owned", Visibility: "cluster", Routing: "leader", RoutingDomain: "platform", Replicas: 1,
			Plugins: []deploymentv1.PluginRef{{ID: "cn.vastplan.platform.configuration.settings", Version: "1.0.0", Channel: "stable"}, {ID: "cn.vastplan.foundation.security.platform-admin-access-policy", Version: "1.0.0", Channel: "stable"}},
		}},
	}
	if err := validateDeploymentContracts(deployment, map[string][]string{"settings": nil}, reader); err != nil {
		t.Fatalf("本地权限辅助贡献应可与 leader 服务共置: %v", err)
	}
}
