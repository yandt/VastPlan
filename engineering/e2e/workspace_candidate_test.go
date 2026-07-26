//go:build e2e

package e2e

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	deploymentv1 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v1"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/nodeagent"
)

func TestWorkspaceCandidateUsesTrustedDerivedVersionAndRealProcess(t *testing.T) {
	repository := newP5FixtureRepository(t)
	const workspaceVersion = "1.5.0-dev.workspace.0123456789abcdef"
	ref := repository.publishChannel(t, p5ManifestSpec{
		id: p5CommonID, version: workspaceVersion, entry: "pipeline-common-workspace", capability: "fixture.composition.common",
	}, "workspace")
	verifier, err := nodeagent.NewSignedArtifactVerifier(repository.trust)
	if err != nil {
		t.Fatal(err)
	}
	pluginRuntime := nodeagent.NewProtocolRuntime("0.1.0", t.Logf)
	t.Cleanup(func() { _ = pluginRuntime.Close() })
	store := nodeagent.NewMemoryStateStore()
	reconciler := &nodeagent.Reconciler{
		NodeID: "workspace-node", Sources: []nodeagent.ArtifactSource{repository}, Verifier: verifier,
		Installer: nodeagent.LocalInstaller{Root: filepath.Join(t.TempDir(), "installed")}, Runtime: pluginRuntime, StateStore: store,
	}
	desired := deploymentv1.DesiredState{
		Version: 1, Revision: 1, Metadata: deploymentv1.Metadata{Name: "workspace", Tenant: "acme"},
		Units: []deploymentv1.Unit{{
			ID: "workspace-service", Kind: "service", Enabled: true, ServiceRole: "backend", Replicas: 1,
			InstancePolicy: "active-active", StateModel: "external-shared", Visibility: "cluster", Routing: "queue", RoutingDomain: "application",
			Plugins: []deploymentv1.PluginRef{repository.lockedPluginRef(t, ref)},
		}},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	result, err := reconciler.Reconcile(ctx, desired)
	if err != nil || !result.Converged {
		t.Fatalf("workspace 候选未收敛: result=%+v err=%v", result, err)
	}
	actual, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	unit := actual.Units["workspace-service"]
	if unit.Phase != nodeagent.PhaseActive || len(unit.PIDs) != 1 || len(unit.Plugins) != 1 || unit.Plugins[0].Version != workspaceVersion || unit.Plugins[0].Channel != "workspace" {
		t.Fatalf("workspace 实际态身份不完整: %+v", unit)
	}
}
