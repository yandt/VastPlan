package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"

	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
	deploymentv2 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v2"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	recoveryv1 "cdsoft.com.cn/VastPlan/contracts/schemas/recovery/v1"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/nodeagent"
	"cdsoft.com.cn/VastPlan/core/shared/go/servicewatchdog"
)

func TestPrepareReconcileBuildsLocalInputs(t *testing.T) {
	options := localAssemblyOptions(t)
	options.labelsRaw = "zone=east,capacity=standard"
	prepared, err := prepareReconcile(options)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.labels["zone"] != "east" || len(prepared.artifacts.sources) != 1 {
		t.Fatalf("本地组合准备结果不完整: %+v", prepared)
	}
	if prepared.inventory != nil || prepared.capsule != nil || prepared.upgrade != nil {
		t.Fatalf("未配置的 Bootstrap/Recovery 依赖不得被隐式装配: %+v", prepared)
	}
}

func TestBuildReconcileRuntimeWiresPoliciesAndLocalOptionalServices(t *testing.T) {
	options := localAssemblyOptions(t)
	prepared, err := prepareReconcile(options)
	if err != nil {
		t.Fatal(err)
	}
	plane, err := newNodeControlPlane(options, t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := buildReconcileRuntime(options, prepared.artifacts, plane, t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.Identity != options.nodeID || runtime.ExecutionPolicy.DefaultPolicy != options.executionPolicy.DefaultPolicy || runtime.LeaderKV != nil {
		t.Fatalf("Protocol Runtime 基础装配漂移: %+v", runtime)
	}
	if runtime.Dependencies.Credentials != nil || runtime.Dependencies.DeploymentPublication != nil || len(runtime.HostServices) != 0 {
		t.Fatalf("未配置的可选服务不得被隐式启用: %+v", runtime.Dependencies)
	}
}

func TestCompositionRootWiresClusterServices(t *testing.T) {
	server := startAssemblyNATS(t)
	options := clusterAssemblyOptions(t, server.ClientURL())
	options.backendPlatformCatalog = writeAssemblyCatalog(t)
	options.frontendDeliveryOrigin = filepath.Join(t.TempDir(), "portal-delivery")
	options.allowDevelopmentPlugins = true

	plane, err := newNodeControlPlane(options, t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = plane.Close() })
	if plane.router == nil || plane.buckets.Controllers == nil || plane.catalogPublisherKV == nil {
		t.Fatalf("集群控制面装配不完整: %+v", plane)
	}

	prepared, err := prepareReconcile(options)
	if err != nil {
		t.Fatal(err)
	}
	runtime := nodeagent.NewProtocolRuntime(version, t.Logf)
	if err := configurePortalHostServices(options, prepared.artifacts, plane, runtime, t.Logf); err != nil {
		t.Fatal(err)
	}
	if len(runtime.HostServices) != 5 {
		t.Fatalf("Portal Host Services 装配不完整: %d", len(runtime.HostServices))
	}
	if err := configureDeploymentServices(options, prepared.artifacts, plane, runtime); err != nil {
		t.Fatal(err)
	}
	if runtime.Dependencies.DeploymentPublication == nil || runtime.Dependencies.PlatformProfileActivation == nil ||
		runtime.Dependencies.DeploymentReadiness == nil || runtime.Dependencies.ConfigurationCatalogs == nil ||
		runtime.Dependencies.ConfigurationAuthorityIssuer == nil || runtime.Dependencies.ConfigurationAuthorityConsumer == nil {
		t.Fatalf("Deployment 服务装配不完整: %+v", runtime.Dependencies)
	}

	options.platformControlProfile = filepath.Join(t.TempDir(), "platform-control.json")
	coordinator, err := configurePlatformControlStartup(options, plane, runtime, t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	if coordinator == nil || coordinator.binding == nil || runtime.Dependencies.PlatformControl == nil || runtime.ReplacementReadiness == nil {
		t.Fatalf("Platform Control 启动装配不完整: coordinator=%+v runtime=%+v", coordinator, runtime.Dependencies)
	}

	prepared.capsule = assemblyRecoveryCapsule()
	options.recoveryStatus = filepath.Join(t.TempDir(), "recovery-status.json")
	recovery, err := buildReconcileRecovery(options, prepared, plane, &servicewatchdog.Notifier{}, t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	if recovery == nil || recovery.controller == nil || recovery.controller.Nodes == nil {
		t.Fatalf("Recovery 组合装配不完整: %+v", recovery)
	}
}

func TestConfigureRuntimeCredentialsWiresNamedBroker(t *testing.T) {
	options := localAssemblyOptions(t)
	options.credentialRoot = t.TempDir()
	plane, err := newNodeControlPlane(options, t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	runtime := nodeagent.NewProtocolRuntime(version, t.Logf)
	if err := configureRuntimeCredentials(options, plane, runtime); err != nil {
		t.Fatal(err)
	}
	if runtime.Dependencies.Credentials == nil || runtime.Dependencies.NodeBootstrap == nil {
		t.Fatalf("目录凭证与节点引导 Broker 必须成对装配: %+v", runtime.Dependencies)
	}
}

func TestBuildNodeReconcilerConsumesPreparedDependencies(t *testing.T) {
	options := localAssemblyOptions(t)
	prepared, err := prepareReconcile(options)
	if err != nil {
		t.Fatal(err)
	}
	plane, err := newNodeControlPlane(options, t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	runtime := nodeagent.NewProtocolRuntime(version, t.Logf)
	reconciler, err := buildNodeReconciler(options, prepared, plane, runtime)
	if err != nil {
		t.Fatal(err)
	}
	if reconciler.NodeID != options.nodeID || len(reconciler.Sources) != 1 || reconciler.Runtime != runtime || reconciler.StateStore == nil || reconciler.Installer == nil {
		t.Fatalf("Node Reconciler 装配不完整: %+v", reconciler)
	}
	if reconciler.References != nil || reconciler.RequireArtifactReferences {
		t.Fatalf("本地仓库不得伪装成托管制品引用路径: %+v", reconciler)
	}
}

func localAssemblyOptions(t *testing.T) reconcileOptions {
	t.Helper()
	root := t.TempDir()
	options, err := parseReconcileOptions([]string{
		"-desired", filepath.Join(root, "desired.json"),
		"-repository", filepath.Join(root, "repository"),
		"-runtime-root", filepath.Join(root, "runtime"),
		"-actual-state", filepath.Join(root, "actual.json"),
		"-node-id", "node-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	return options
}

func clusterAssemblyOptions(t *testing.T, url string) reconcileOptions {
	t.Helper()
	root := t.TempDir()
	options, err := parseReconcileOptions([]string{
		"-nats-url", url,
		"-nats-allow-insecure",
		"-nats-bootstrap",
		"-repository", filepath.Join(root, "repository"),
		"-runtime-root", filepath.Join(root, "runtime"),
		"-actual-state", filepath.Join(root, "actual.json"),
		"-node-id", "node-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	return options
}

func startAssemblyNATS(t *testing.T) *natsserver.Server {
	t.Helper()
	server, err := natsserver.NewServer(&natsserver.Options{
		JetStream: true,
		StoreDir:  filepath.Join(t.TempDir(), "jetstream"),
		Host:      "127.0.0.1",
		Port:      -1,
		NoLog:     true,
		NoSigs:    true,
	})
	if err != nil {
		t.Fatal(err)
	}
	go server.Start()
	if !server.ReadyForConnections(5 * time.Second) {
		server.Shutdown()
		t.Fatal("内嵌 NATS 未就绪")
	}
	t.Cleanup(server.Shutdown)
	return server
}

func writeAssemblyCatalog(t *testing.T) string {
	t.Helper()
	profile := backendcompositionv1.PlatformProfile{
		Document:            compositioncommonv1.Document{Version: 1, Revision: 1, ID: "backend-default"},
		Target:              compositioncommonv1.Target{Kernel: compositioncommonv1.KernelBackend},
		ServiceClasses:      []string{"application.backend"},
		ProductCapabilities: []backendcompositionv1.ProductCapability{},
		ServiceBaselines:    []backendcompositionv1.ServiceBaseline{},
		Services:            []deploymentv2.ServiceUnit{},
	}
	catalog := backendcompositionv1.BackendPlatformCatalog{
		Document: compositioncommonv1.Document{Version: 1, Revision: 1, ID: "backend-test"},
		Profiles: []backendcompositionv1.PlatformProfile{profile},
		Bindings: []backendcompositionv1.BackendPlatformBinding{{
			TenantID: "acme", DeploymentName: "services",
			PlatformProfile: compositioncommonv1.Ref{ID: profile.ID, Revision: profile.Revision, Digest: profile.Digest()},
		}},
	}
	raw, err := json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "backend-platform-catalog.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func assemblyRecoveryCapsule() *recoveryv1.Capsule {
	return &recoveryv1.Capsule{
		Version: recoveryv1.Version,
		ID:      "platform",
		Inventory: recoveryv1.InventoryBinding{
			RepositoryID: "seed",
			Generation:   1,
		},
		Artifacts: []recoveryv1.Artifact{{
			Ref:    pluginv1.ArtifactRef{PluginID: "repository", Version: "1.0.0", Channel: "stable"},
			SHA256: strings.Repeat("a", 64),
		}},
		Stages: []recoveryv1.Stage{
			{ID: recoveryv1.StageRecovery, Units: []recoveryv1.UnitRequirement{{ID: "repository", MinReady: 1}}},
			{ID: recoveryv1.StageControlPlane, Units: []recoveryv1.UnitRequirement{{ID: "deployment", MinReady: 1}}},
			{ID: recoveryv1.StagePlatform, Units: []recoveryv1.UnitRequirement{{ID: "database", MinReady: 1}}},
		},
	}
}
