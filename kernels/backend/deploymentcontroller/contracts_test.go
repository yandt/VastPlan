package deploymentcontroller

import (
	"errors"
	"strings"
	"testing"

	deploymentv1 "cdsoft.com.cn/VastPlan/schemas/deployment/v1"
	deploymentv2 "cdsoft.com.cn/VastPlan/schemas/deployment/v2"
	pluginv1 "cdsoft.com.cn/VastPlan/schemas/plugin/v1"
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
