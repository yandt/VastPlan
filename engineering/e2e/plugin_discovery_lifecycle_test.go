//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	artifactrepositoryv1 "cdsoft.com.cn/VastPlan/contracts/schemas/artifactrepository/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginreconcile"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/artifactrepository/locallibrary"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/artifacttrust"
)

func TestUnifiedPluginDiscoveryLifecycle(t *testing.T) {
	v1Inventory, v1Index, v1 := lifecycleSnapshots(t, "1.0.0", "stable", "a")
	v1Selection := explicitSelection(t, v1Inventory, v1Index, v1.Ref)
	created := reconciliation(t, v1Selection, v1Index, nil)
	if created.Actions[0].Operation != pluginv1.ReconcileActivate {
		t.Fatalf("新增插件未形成 activate: %+v", created.Actions)
	}

	v2Inventory, v2Index, v2 := lifecycleSnapshots(t, "2.0.0", "stable", "b")
	v2Selection := explicitSelection(t, v2Inventory, v2Index, v2.Ref)
	updated := reconciliation(t, v2Selection, v2Index, []pluginv1.PluginArtifactIdentity{v1})
	if updated.Actions[0].Operation != pluginv1.ReconcileReplace || updated.Actions[0].Strategy != "backend.rolling-generation" {
		t.Fatalf("更新插件未形成滚动替换: %+v", updated.Actions)
	}

	emptyInventory, emptyIndex := emptyLifecycleSnapshots(t, 3)
	removedSelection, err := (pluginv1.DevelopmentActivationPolicy{PolicyID: "development", Kernel: pluginv1.PluginTargetBackend}).Select(emptyInventory, emptyIndex)
	if err != nil {
		t.Fatal(err)
	}
	removed := reconciliation(t, removedSelection, emptyIndex, []pluginv1.PluginArtifactIdentity{v2})
	if removed.Actions[0].Operation != pluginv1.ReconcileDeactivate || removed.Actions[0].Strategy != "backend.drain-stop" {
		t.Fatalf("撤销插件必须先 Drain: %+v", removed.Actions)
	}

	rolledBack := reconciliation(t, v1Selection, v1Index, []pluginv1.PluginArtifactIdentity{v2})
	if rolledBack.Actions[0].Operation != pluginv1.ReconcileReplace || rolledBack.Actions[0].Candidate.Ref.Version != "1.0.0" {
		t.Fatalf("回退未恢复旧精确制品: %+v", rolledBack.Actions)
	}
	recovered := reconciliation(t, v1Selection, v1Index, []pluginv1.PluginArtifactIdentity{v2})
	if recovered.Digest != rolledBack.Digest {
		t.Fatal("相同 Inventory/Selection/Current 在重启后必须恢复相同计划")
	}

	assertAmbiguousWorkspaceRejected(t)
	assertRemoteArtifactImported(t, v1)
}

func lifecycleSnapshots(t *testing.T, version, channel, digestCharacter string) (pluginv1.PluginInventorySnapshot, pluginv1.ContributionIndexSnapshot, pluginv1.PluginArtifactIdentity) {
	t.Helper()
	manifest := pluginv1.Manifest{ID: "cn.vastplan.lifecycle", Version: version, Publisher: "vastplan", Engines: map[string]string{"backend": "^1.0"}, Entry: map[string]string{"backend": "bin/plugin"}, Contributes: map[string]json.RawMessage{"backend": json.RawMessage(`{"tools":[{"id":"lifecycle.tool","service_role":"backend"}]}`)}}
	artifact := pluginv1.Artifact{PluginID: manifest.ID, Version: version, Channel: channel, SHA256: strings.Repeat(digestCharacter, 64)}
	value := pluginv1.VerifiedArtifactManifest{Artifact: artifact, Manifest: manifest}
	inventory, err := pluginv1.BuildPluginInventory(1, strings.Repeat("c", 64), []pluginv1.VerifiedArtifactManifest{value})
	if err != nil {
		t.Fatal(err)
	}
	index, err := pluginv1.BuildContributionIndex(inventory, []pluginv1.VerifiedArtifactManifest{value})
	if err != nil {
		t.Fatal(err)
	}
	return inventory, index, inventory.Plugins[0].Artifact
}

func emptyLifecycleSnapshots(t *testing.T, generation uint64) (pluginv1.PluginInventorySnapshot, pluginv1.ContributionIndexSnapshot) {
	t.Helper()
	inventory, err := pluginv1.BuildPluginInventory(generation, strings.Repeat("d", 64), nil)
	if err != nil {
		t.Fatal(err)
	}
	index, err := pluginv1.BuildContributionIndex(inventory, nil)
	if err != nil {
		t.Fatal(err)
	}
	return inventory, index
}

func explicitSelection(t *testing.T, inventory pluginv1.PluginInventorySnapshot, index pluginv1.ContributionIndexSnapshot, ref pluginv1.ArtifactRef) pluginv1.ActivationSelection {
	t.Helper()
	selection, err := (pluginv1.ExplicitActivationPolicy{PolicyID: "profile", Kernel: pluginv1.PluginTargetBackend, Roots: []pluginv1.ArtifactRef{ref}}).Select(inventory, index)
	if err != nil {
		t.Fatal(err)
	}
	return selection
}

func reconciliation(t *testing.T, selection pluginv1.ActivationSelection, index pluginv1.ContributionIndexSnapshot, current []pluginv1.PluginArtifactIdentity) pluginv1.ReconciliationPlan {
	t.Helper()
	plan, err := pluginv1.PlanReconciliation(selection, index, current, pluginreconcile.BackendAdapter())
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func assertAmbiguousWorkspaceRejected(t *testing.T) {
	t.Helper()
	oneInventory, _, _ := lifecycleSnapshots(t, "1.0.0-dev.workspace.one", "workspace", "e")
	twoInventory, _, _ := lifecycleSnapshots(t, "1.0.0-dev.workspace.two", "workspace", "f")
	values := []pluginv1.PluginInventoryItem{oneInventory.Plugins[0], twoInventory.Plugins[0]}
	inventory := pluginv1.PluginInventorySnapshot{SchemaVersion: 1, Generation: 2, SourceDigest: strings.Repeat("1", 64), Plugins: values}
	// Rebuild through the public projector so the digest remains canonical.
	manifests := []pluginv1.VerifiedArtifactManifest{}
	for _, item := range values {
		manifest := pluginv1.Manifest{ID: item.Artifact.Ref.PluginID, Version: item.Artifact.Ref.Version, Publisher: item.Artifact.Publisher, Engines: map[string]string{"backend": "^1.0"}, Entry: map[string]string{"backend": "bin/plugin"}, Contributes: map[string]json.RawMessage{"backend": json.RawMessage(`{"tools":[]}`)}}
		manifests = append(manifests, pluginv1.VerifiedArtifactManifest{Artifact: pluginv1.Artifact{PluginID: manifest.ID, Version: manifest.Version, Channel: item.Artifact.Ref.Channel, SHA256: item.Artifact.SHA256}, Manifest: manifest})
	}
	var err error
	inventory, err = pluginv1.BuildPluginInventory(2, strings.Repeat("1", 64), manifests)
	if err != nil {
		t.Fatal(err)
	}
	index, err := pluginv1.BuildContributionIndex(inventory, manifests)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (pluginv1.DevelopmentActivationPolicy{PolicyID: "development", Kernel: pluginv1.PluginTargetBackend}).Select(inventory, index); err == nil {
		t.Fatal("Provider 候选歧义不得按扫描顺序选择")
	}
}

type lifecycleRepository struct {
	profile  artifactrepositoryv1.Profile
	receipt  artifactrepositoryv1.Receipt
	envelope artifacttrust.Envelope
	imported bool
}

func (r *lifecycleRepository) Profile() artifactrepositoryv1.Profile { return r.profile }
func (r *lifecycleRepository) ReadExact(context.Context, pluginv1.ArtifactRef) (artifacttrust.Envelope, error) {
	return r.envelope, nil
}
func (r *lifecycleRepository) Publish(context.Context, artifacttrust.Envelope) (artifactrepositoryv1.Receipt, error) {
	return r.receipt, nil
}
func (r *lifecycleRepository) CatalogSnapshot(context.Context) (artifactrepositoryv1.CatalogSnapshot, error) {
	return artifactrepositoryv1.CatalogSnapshot{SchemaVersion: 1, RepositoryID: r.profile.ID, Protocol: r.profile.Protocol, ProfileDigest: r.profile.Digest(), Revision: r.receipt.Revision, Items: []artifactrepositoryv1.Receipt{r.receipt}}, nil
}
func (r *lifecycleRepository) ImportExact(_ context.Context, _ artifactrepositoryv1.Profile, source artifactrepositoryv1.Receipt, _ artifacttrust.Envelope) (artifactrepositoryv1.ImportRecord, error) {
	r.imported = true
	destination := artifactrepositoryv1.Receipt{SchemaVersion: 1, RepositoryID: r.profile.ID, Protocol: r.profile.Protocol, ProfileDigest: r.profile.Digest(), Ref: source.Ref, SHA256: source.SHA256, Revision: 11}
	return artifactrepositoryv1.ImportRecord{SchemaVersion: 1, Source: source, Destination: destination, ImportedAt: time.Now().UTC()}, nil
}

func assertRemoteArtifactImported(t *testing.T, identity pluginv1.PluginArtifactIdentity) {
	t.Helper()
	remote, err := artifactrepositoryv1.ValidateProfile(artifactrepositoryv1.Profile{Version: 1, ID: "remote", Protocol: artifactrepositoryv1.ProtocolRemote, Endpoint: "https://repo.example", Channels: []string{"stable", "testing"}})
	if err != nil {
		t.Fatal(err)
	}
	local, err := artifactrepositoryv1.ValidateProfile(artifactrepositoryv1.Profile{Version: 1, ID: "local", Protocol: artifactrepositoryv1.ProtocolLocalTest, Endpoint: "unix:///tmp/local.sock", Channels: []string{"stable", "testing"}, DevelopmentOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	receipt := artifactrepositoryv1.Receipt{SchemaVersion: 1, RepositoryID: remote.ID, Protocol: remote.Protocol, ProfileDigest: remote.Digest(), Ref: identity.Ref, SHA256: identity.SHA256, Revision: 7}
	source := &lifecycleRepository{profile: remote, receipt: receipt, envelope: artifacttrust.Envelope{Artifact: pluginv1.Artifact{PluginID: identity.Ref.PluginID, Version: identity.Ref.Version, Channel: identity.Ref.Channel, SHA256: identity.SHA256}}}
	destination := &lifecycleRepository{profile: local}
	record, err := locallibrary.ImportExact(context.Background(), source, destination, identity.Ref)
	if err != nil || !destination.imported || record.Source.Ref != identity.Ref || record.Destination.SHA256 != identity.SHA256 {
		t.Fatalf("Remote -> Local Plugin Library 导入失败: %+v %v", record, err)
	}
}
