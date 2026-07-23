package bootstrapupgrade

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/pluginservice"
	"cdsoft.com.cn/VastPlan/core/shared/go/bootstrapinventory"
)

const repositoryPluginID = "cn.vastplan.platform.artifacts.repository"

type memoryInventoryStore struct{ inventory bootstrapinventory.Inventory }

func (s *memoryInventoryStore) Load() (bootstrapinventory.Inventory, error) { return s.inventory, nil }

func (s *memoryInventoryStore) Update(expected uint64, next bootstrapinventory.Inventory) (bootstrapinventory.Inventory, error) {
	if s.inventory.Generation != expected {
		return bootstrapinventory.Inventory{}, errors.New("cas conflict")
	}
	s.inventory = next
	return next, nil
}

type acceptingSeed struct {
	publishes                          int
	provenance, provenanceVerification []byte
}

func (s *acceptingSeed) PublishWithProvenance(value pluginservice.Attestation, _ []byte, provenance, verification []byte) (pluginv1.Artifact, error) {
	s.publishes++
	s.provenance, s.provenanceVerification = append([]byte(nil), provenance...), append([]byte(nil), verification...)
	return value.Artifact, nil
}

func bootstrapItem(version, digest string) bootstrapinventory.Item {
	return bootstrapinventory.Item{
		Ref:    pluginv1.ArtifactRef{PluginID: repositoryPluginID, Version: version, Channel: "stable"},
		SHA256: digest,
	}
}

func initialInventory(t *testing.T) bootstrapinventory.Inventory {
	t.Helper()
	old := bootstrapItem("1.0.0", strings.Repeat("1", 64))
	value, err := bootstrapinventory.Normalize(bootstrapinventory.Inventory{
		Version: bootstrapinventory.Version, Generation: 7, RepositoryID: "local-seed",
		Seed: []bootstrapinventory.Item{old}, LastKnownGood: []bootstrapinventory.Item{old},
	})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func upgradeCandidate(t *testing.T, version, digest string) Candidate {
	t.Helper()
	artifact := pluginv1.Artifact{
		PluginID: repositoryPluginID, Version: version, Channel: "stable", SHA256: digest, Size: 1,
	}
	proof, err := json.Marshal(pluginservice.Attestation{SchemaVersion: "v1", Artifact: artifact})
	if err != nil {
		t.Fatal(err)
	}
	return Candidate{Artifact: artifact, PackageBytes: []byte{1}, Proof: proof}
}

func TestControllerPrepareAndCommitAdvanceSeedThenLKG(t *testing.T) {
	store := &memoryInventoryStore{inventory: initialInventory(t)}
	seed := &acceptingSeed{}
	controller, err := New(store, seed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controller.Begin(store.inventory.LastKnownGood); err != nil {
		t.Fatal(err)
	}
	candidate := upgradeCandidate(t, "2.0.0", strings.Repeat("2", 64))
	candidate.Provenance, candidate.ProvenanceVerification = []byte("provenance"), []byte("verification")
	prepared, err := controller.Prepare(context.Background(), []Candidate{candidate})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Generation != 8 || len(prepared.Seed) != 2 || prepared.LastKnownGood[0].Ref.Version != "1.0.0" || seed.publishes != 1 {
		t.Fatalf("Prepare 只能扩展 Seed: %+v publishes=%d", prepared, seed.publishes)
	}
	if string(seed.provenance) != "provenance" || string(seed.provenanceVerification) != "verification" {
		t.Fatal("Bootstrap Seed 镜像丢失来源证明 sidecar")
	}
	committed, err := controller.Commit(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if committed.Generation != 9 || committed.LastKnownGood[0].Ref.Version != "2.0.0" || len(committed.Seed) != 2 {
		t.Fatalf("Commit 应在健康后推进 LKG 且保留旧 Seed: %+v", committed)
	}
	if repeated, err := controller.Commit(context.Background()); err != nil || repeated.Generation != committed.Generation {
		t.Fatalf("Commit 必须幂等: %+v err=%v", repeated, err)
	}
}

func TestControllerRestartRecoversActivatedCandidate(t *testing.T) {
	store := &memoryInventoryStore{inventory: initialInventory(t)}
	first, _ := New(store, &acceptingSeed{})
	_, _ = first.Begin(nil)
	candidate := upgradeCandidate(t, "2.0.0", strings.Repeat("2", 64))
	prepared, err := first.Prepare(context.Background(), []Candidate{candidate})
	if err != nil {
		t.Fatal(err)
	}

	restarted, _ := New(store, &acceptingSeed{})
	installed := bootstrapItem("2.0.0", candidate.Artifact.SHA256)
	if _, err := restarted.Begin([]bootstrapinventory.Item{installed}); err != nil {
		t.Fatal(err)
	}
	committed, err := restarted.Commit(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if prepared.LastKnownGood[0].Ref.Version != "1.0.0" || committed.LastKnownGood[0] != installed {
		t.Fatalf("重启恢复不应提前覆盖 LKG: prepared=%+v committed=%+v", prepared, committed)
	}
}

func TestControllerRestartIgnoresActualStateOlderThanCommittedLKG(t *testing.T) {
	store := &memoryInventoryStore{inventory: initialInventory(t)}
	v2 := bootstrapItem("2.0.0", strings.Repeat("2", 64))
	store.inventory.Generation++
	store.inventory.Seed = append(store.inventory.Seed, v2)
	store.inventory.LastKnownGood = []bootstrapinventory.Item{v2}
	var err error
	store.inventory, err = bootstrapinventory.Normalize(store.inventory)
	if err != nil {
		t.Fatal(err)
	}

	restarted, _ := New(store, &acceptingSeed{})
	staleActual := bootstrapItem("1.0.0", strings.Repeat("1", 64))
	if _, err := restarted.Begin([]bootstrapinventory.Item{staleActual}); err != nil {
		t.Fatalf("LKG 提交后遗留的旧 ActualState 不得被解释为降级事务: %v", err)
	}
	committed, err := restarted.Commit(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if committed.LastKnownGood[0] != v2 {
		t.Fatalf("旧 ActualState 不得回退 LKG: %+v", committed.LastKnownGood)
	}
}

func TestControllerRejectsAutomaticDowngradeAndChannelSwitch(t *testing.T) {
	store := &memoryInventoryStore{inventory: initialInventory(t)}
	controller, _ := New(store, &acceptingSeed{})
	_, _ = controller.Begin(nil)
	for _, candidate := range []Candidate{
		upgradeCandidate(t, "0.9.0", strings.Repeat("2", 64)),
		upgradeCandidate(t, "2.0.0", strings.Repeat("3", 64)),
	} {
		if candidate.Artifact.Version == "2.0.0" {
			candidate.Artifact.Channel = "testing"
			candidate.Proof = mustProof(t, candidate.Artifact)
		}
		if _, err := controller.Prepare(context.Background(), []Candidate{candidate}); err == nil {
			t.Fatalf("自动降级/跨通道必须拒绝: %+v", candidate.Artifact)
		}
	}
	if store.inventory.Generation != 7 {
		t.Fatalf("拒绝候选不得改写 Inventory: %+v", store.inventory)
	}
}

func TestControllerCommitCannotOverwriteNewerConcurrentLKG(t *testing.T) {
	store := &memoryInventoryStore{inventory: initialInventory(t)}
	controller, _ := New(store, &acceptingSeed{})
	_, _ = controller.Begin(nil)
	v2 := upgradeCandidate(t, "2.0.0", strings.Repeat("2", 64))
	if _, err := controller.Prepare(context.Background(), []Candidate{v2}); err != nil {
		t.Fatal(err)
	}
	v3 := bootstrapItem("3.0.0", strings.Repeat("3", 64))
	concurrent := store.inventory
	concurrent.Generation++
	concurrent.Seed = append(concurrent.Seed, v3)
	concurrent.LastKnownGood = []bootstrapinventory.Item{v3}
	var err error
	store.inventory, err = bootstrapinventory.Normalize(concurrent)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controller.Commit(context.Background()); err == nil {
		t.Fatal("较旧事务不得覆盖并发提交的更新 LKG")
	}
	if store.inventory.LastKnownGood[0] != v3 {
		t.Fatalf("并发 LKG 被旧事务覆盖: %+v", store.inventory)
	}
}

func mustProof(t *testing.T, artifact pluginv1.Artifact) []byte {
	t.Helper()
	raw, err := json.Marshal(pluginservice.Attestation{SchemaVersion: "v1", Artifact: artifact})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
