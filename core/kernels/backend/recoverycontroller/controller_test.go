package recoverycontroller

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	recoveryv1 "cdsoft.com.cn/VastPlan/contracts/schemas/recovery/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/bootstrapinventory"
	"cdsoft.com.cn/VastPlan/core/shared/go/controlplane"
)

type recordingLease struct{ report recoveryv1.NodeReport }

func (l *recordingLease) UpdateRecovery(report recoveryv1.NodeReport) error {
	l.report = recoveryv1.CloneNodeReport(report)
	return nil
}

func TestControllerProjectsRuntimeObservationToFileLeaseAndHTTP(t *testing.T) {
	now := time.Now().UTC()
	statusFile := filepath.Join(t.TempDir(), "recovery-status.json")
	controller, err := New(testCapsule(1), "node-a", "acme", "platform", statusFile)
	if err != nil {
		t.Fatal(err)
	}
	controller.Now = func() time.Time { return now }
	lease := &recordingLease{}
	if err := controller.AttachLease(lease); err != nil {
		t.Fatal(err)
	}
	if err := controller.Observe(readyRuntimeObservation("node-a", now)); err != nil {
		t.Fatal(err)
	}
	if lease.report.Units["repository"].Status != recoveryv1.UnitReady {
		t.Fatalf("signed lease report not updated: %+v", lease.report)
	}
	info, err := os.Stat(statusFile)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("status file must be owner-only: mode=%v", info.Mode().Perm())
	}
	server, err := StartServer("127.0.0.1:0", controller)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close(context.Background()) })
	response, err := http.Get("http://" + server.Addr() + "/v1/recovery/status")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("unexpected recovery response: %d %+v", response.StatusCode, response.Header)
	}
	if _, err := StartServer("0.0.0.0:0", controller); err == nil {
		t.Fatal("non-loopback recovery listener must fail")
	}
}

func TestControllerAggregatesExactFreshNodeReports(t *testing.T) {
	buckets := startRecoveryNATS(t)
	now := time.Now().UTC()
	capsule := testCapsule(2)
	for _, nodeID := range []string{"node-a", "node-b"} {
		report := recoveryv1.BuildNodeReport(capsule, recoveryv1.RuntimeObservation{
			NodeID: nodeID, ObservedRevision: 4, AppliedRevision: 4, UpdatedAt: now,
			Units: map[string]recoveryv1.UnitObservation{
				"repository": {Phase: "active", Readiness: "ready"},
				"deployment": {Phase: "active", Readiness: "ready"},
				"database":   {Phase: "active", Readiness: "ready"},
			},
		}, recoveryv1.EvaluationPolicy{Now: now})
		record := controlplane.NodeRecord{SchemaVersion: 4, NodeID: nodeID, TenantID: "acme", Deployment: "platform", UpdatedAt: now, Recovery: &report}
		raw, _ := json.Marshal(record)
		if _, err := buckets.Nodes.Put(context.Background(), controlplane.NodeKey("acme", "platform", nodeID), raw); err != nil {
			t.Fatal(err)
		}
		if nodeID == "node-a" {
			if _, err := buckets.Nodes.Put(context.Background(), controlplane.DeploymentKey("acme", "platform")+".nodes.replayed", raw); err != nil {
				t.Fatal(err)
			}
		}
	}
	controller, err := New(capsule, "node-a", "acme", "platform", filepath.Join(t.TempDir(), "status.json"))
	if err != nil {
		t.Fatal(err)
	}
	controller.Nodes = buckets.Nodes
	controller.Now = func() time.Time { return now }
	controller.Verify = func(record controlplane.NodeRecord) error { return record.ValidateBasic() }
	status, err := controller.Status(context.Background())
	if err != nil || status.Overall != recoveryv1.OverallPlatformReady || status.Nodes != 2 || status.Scope != "cluster" || !status.ClusterAvailable {
		t.Fatalf("cluster quorum not aggregated: status=%+v err=%v", status, err)
	}
}

func TestControllerFallsBackToExplicitLocalScopeWhenClusterUnavailable(t *testing.T) {
	buckets := startRecoveryNATS(t)
	now := time.Now().UTC()
	controller, err := New(testCapsule(1), "node-a", "acme", "platform", filepath.Join(t.TempDir(), "status.json"))
	if err != nil {
		t.Fatal(err)
	}
	controller.Nodes = buckets.Nodes
	controller.Verify = func(record controlplane.NodeRecord) error { return record.ValidateBasic() }
	controller.Now = func() time.Time { return now }
	if err := controller.Observe(readyRuntimeObservation("node-a", now)); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	status, err := controller.Status(ctx)
	if err != nil || status.Overall != recoveryv1.OverallPlatformReady || status.Scope != "local" || status.ClusterAvailable || status.Nodes != 1 {
		t.Fatalf("cluster failure must retain an explicit local projection: status=%+v err=%v", status, err)
	}
}

func TestLoadCapsuleAndInventoryFailClosedOnTampering(t *testing.T) {
	capsule := testCapsule(1)
	path := filepath.Join(t.TempDir(), "capsule.json")
	raw, _ := json.Marshal(capsule)
	if err := os.WriteFile(path, raw, 0o666); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o666); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCapsule(path); err == nil {
		t.Fatal("group/other writable Capsule must fail")
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCapsule(path); err != nil {
		t.Fatal(err)
	}
	inventory := bootstrapinventory.Inventory{
		Version: bootstrapinventory.Version, Generation: capsule.Inventory.Generation, RepositoryID: capsule.Inventory.RepositoryID,
		Seed:          []bootstrapinventory.Item{{Ref: capsule.Artifacts[0].Ref, SHA256: capsule.Artifacts[0].SHA256}},
		LastKnownGood: []bootstrapinventory.Item{{Ref: capsule.Artifacts[0].Ref, SHA256: strings.Repeat("b", 64)}},
	}
	if err := ValidateInventory(capsule, inventory); err == nil {
		t.Fatal("Capsule/LKG digest drift must fail")
	}
}

func testCapsule(minReady uint16) recoveryv1.Capsule {
	return recoveryv1.Capsule{
		Version: recoveryv1.Version, ID: "platform", Inventory: recoveryv1.InventoryBinding{RepositoryID: "seed", Generation: 3},
		Artifacts: []recoveryv1.Artifact{{Ref: pluginv1.ArtifactRef{PluginID: "repository", Version: "1.0.0", Channel: "stable"}, SHA256: strings.Repeat("a", 64)}},
		Stages: []recoveryv1.Stage{
			{ID: recoveryv1.StageRecovery, Units: []recoveryv1.UnitRequirement{{ID: "repository", MinReady: minReady}}},
			{ID: recoveryv1.StageControlPlane, Units: []recoveryv1.UnitRequirement{{ID: "deployment", MinReady: minReady}}},
			{ID: recoveryv1.StagePlatform, Units: []recoveryv1.UnitRequirement{{ID: "database", MinReady: minReady}}},
		},
	}
}

func readyRuntimeObservation(nodeID string, now time.Time) recoveryv1.RuntimeObservation {
	return recoveryv1.RuntimeObservation{
		NodeID: nodeID, ObservedRevision: 2, AppliedRevision: 2, UpdatedAt: now,
		Units: map[string]recoveryv1.UnitObservation{
			"repository": {Phase: "active", Readiness: "ready"},
			"deployment": {Phase: "active", Readiness: "ready"},
			"database":   {Phase: "active", Readiness: "ready"},
		},
	}
}

func startRecoveryNATS(t *testing.T) controlplane.Buckets {
	t.Helper()
	server, err := natsserver.NewServer(&natsserver.Options{JetStream: true, StoreDir: filepath.Join(t.TempDir(), "jetstream"), Host: "127.0.0.1", Port: -1, NoLog: true, NoSigs: true})
	if err != nil {
		t.Fatal(err)
	}
	go server.Start()
	if !server.ReadyForConnections(10 * time.Second) {
		t.Fatal("embedded NATS not ready")
	}
	if _, ok := server.Addr().(*net.TCPAddr); !ok {
		t.Fatal("embedded NATS not on TCP")
	}
	t.Cleanup(func() { server.Shutdown(); server.WaitForShutdown() })
	connection, err := nats.Connect(server.ClientURL())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(connection.Close)
	js, err := jetstream.New(connection)
	if err != nil {
		t.Fatal(err)
	}
	buckets, err := controlplane.EnsureBuckets(context.Background(), js, 1, jetstream.MemoryStorage)
	if err != nil {
		t.Fatal(err)
	}
	return buckets
}
