package deploymentcontroller

import (
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	deploymentv1 "cdsoft.com.cn/VastPlan/schemas/deployment/v1"
	deploymentv2 "cdsoft.com.cn/VastPlan/schemas/deployment/v2"
	"cdsoft.com.cn/VastPlan/shared/go/controlplane"
)

func TestSchedulerReplicasPlacementScaleAndNodeLoss(t *testing.T) {
	_, buckets := startSchedulerNATS(t)
	ctx := context.Background()
	leases := map[string]*controlplane.NodeLease{}
	for nodeID, labels := range map[string]map[string]string{
		"node-a": {"region": "cn"}, "node-b": {"region": "cn"}, "node-c": {"region": "us"},
	} {
		lease, err := controlplane.StartNodeLease(ctx, buckets.Nodes, nodeID, labels, controlplane.NodeLeaseOptions{})
		if err != nil {
			t.Fatal(err)
		}
		leases[nodeID] = lease
	}
	t.Cleanup(func() {
		for _, lease := range leases {
			_ = lease.Close(context.Background())
		}
	})
	scheduler := Scheduler{Nodes: buckets.Nodes, Assignments: buckets.Assignments}
	deployment := testDeployment(2)
	plan, err := scheduler.Reconcile(ctx, deployment)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Generation != 1 || countUnit(plan.Assignments, "api") != 2 {
		t.Fatalf("replicas=2 调度错误: generation=%d assignments=%+v", plan.Generation, plan.Assignments)
	}
	if len(plan.Assignments["node-c"].Units) != 0 {
		t.Fatal("region=us 节点不应收到 region=cn unit")
	}
	idempotent, err := scheduler.Reconcile(ctx, deployment)
	if err != nil || idempotent.Generation != plan.Generation {
		t.Fatalf("相同拓扑重复调度必须幂等: plan=%+v err=%v", idempotent, err)
	}

	deployment.Revision = 2
	deployment.Units[0].Replicas = 1
	scaled, err := scheduler.Reconcile(ctx, deployment)
	if err != nil {
		t.Fatal(err)
	}
	if scaled.Generation != 2 || countUnit(scaled.Assignments, "api") != 1 {
		t.Fatalf("缩容结果错误: %+v", scaled)
	}
	selected := selectedNode(scaled.Assignments, "api")
	if selected == "" {
		t.Fatal("缩容后没有选中节点")
	}
	if err := leases[selected].Close(ctx); err != nil {
		t.Fatal(err)
	}
	delete(leases, selected)
	rescheduled, err := scheduler.Reconcile(ctx, deployment)
	if err != nil {
		t.Fatal(err)
	}
	if got := selectedNode(rescheduled.Assignments, "api"); got == "" || got == selected {
		t.Fatalf("节点退出后未漂移到另一 CN 节点: old=%s new=%s", selected, got)
	}
	if rescheduled.Generation != 3 {
		t.Fatalf("拓扑变化应推进 assignment generation: %d", rescheduled.Generation)
	}

	deployment.Revision = 3
	deployment.Units[0].Replicas = 2
	if _, err := scheduler.Reconcile(ctx, deployment); err == nil {
		t.Fatal("只有一个匹配节点时 replicas=2 必须在写入前 fail-closed")
	}
	kept := loadAssignment(t, buckets.Assignments, deployment, selectedNode(rescheduled.Assignments, "api"))
	if len(kept.Units) != 1 || kept.Revision != rescheduled.Generation {
		t.Fatalf("容量不足不应破坏最近健康计划: %+v", kept)
	}
}

func TestControllerWatchesDeploymentUpdates(t *testing.T) {
	_, buckets := startSchedulerNATS(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for nodeID := range map[string]struct{}{"node-a": {}, "node-b": {}} {
		lease, err := controlplane.StartNodeLease(ctx, buckets.Nodes, nodeID, map[string]string{"region": "cn"}, controlplane.NodeLeaseOptions{})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := lease.Close(context.Background()); err != nil {
				t.Errorf("关闭节点租约: %v", err)
			}
		})
	}
	deployment := testDeployment(1)
	raw, _ := json.Marshal(deployment)
	key := controlplane.DeploymentKey(deployment.Metadata.Tenant, deployment.Metadata.Name)
	if _, _, err := controlplane.ApplyDeployment(ctx, buckets.Deployments, key, raw); err != nil {
		t.Fatal(err)
	}
	controller := Controller{
		Deployments: buckets.Deployments, DeploymentKey: key, Interval: time.Hour,
		Scheduler: Scheduler{Nodes: buckets.Nodes, Assignments: buckets.Assignments}, Logf: t.Logf,
		Leaders: buckets.Controllers, Identity: "controller-a",
		Election: controlplane.LeaderElectionOptions{LeaseDuration: time.Second, RenewEvery: 100 * time.Millisecond, RetryEvery: 20 * time.Millisecond},
	}
	done := make(chan error, 1)
	go func() { done <- controller.Run(ctx) }()
	waitAssignedReplicas(t, buckets.Assignments, deployment, 1)
	deployment.Revision = 2
	deployment.Units[0].Replicas = 2
	raw, _ = json.Marshal(deployment)
	if _, _, err := controlplane.ApplyDeployment(ctx, buckets.Deployments, key, raw); err != nil {
		t.Fatal(err)
	}
	waitAssignedReplicas(t, buckets.Assignments, deployment, 2)
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("controller 未响应 context 取消")
	}
}

func TestControllerLeaderFailoverKeepsSingleActiveWriter(t *testing.T) {
	_, buckets := startSchedulerNATS(t)
	rootCtx := context.Background()
	lease, err := controlplane.StartNodeLease(rootCtx, buckets.Nodes, "node-a", map[string]string{"region": "cn"}, controlplane.NodeLeaseOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lease.Close(context.Background()) })
	deployment := testDeployment(1)
	raw, _ := json.Marshal(deployment)
	key := controlplane.DeploymentKey(deployment.Metadata.Tenant, deployment.Metadata.Name)
	if _, _, err := controlplane.ApplyDeployment(rootCtx, buckets.Deployments, key, raw); err != nil {
		t.Fatal(err)
	}
	options := controlplane.LeaderElectionOptions{
		LeaseDuration: 500 * time.Millisecond, RenewEvery: 50 * time.Millisecond, RetryEvery: 20 * time.Millisecond,
	}
	leaderA, leaderB := make(chan struct{}, 2), make(chan struct{}, 2)
	newController := func(identity string, elected chan<- struct{}) Controller {
		return Controller{
			Deployments: buckets.Deployments, DeploymentKey: key, Interval: time.Hour,
			Scheduler: Scheduler{Nodes: buckets.Nodes, Assignments: buckets.Assignments},
			Leaders:   buckets.Controllers, Identity: identity, Election: options,
			Logf: func(format string, _ ...any) {
				if strings.Contains(format, "获得领导权") {
					select {
					case elected <- struct{}{}:
					default:
					}
				}
			},
		}
	}
	ctxA, cancelA := context.WithCancel(rootCtx)
	doneA := make(chan error, 1)
	go func() { doneA <- newController("controller-a", leaderA).Run(ctxA) }()
	select {
	case <-leaderA:
	case <-time.After(2 * time.Second):
		t.Fatal("controller-a 未获得初始领导权")
	}
	ctxB, cancelB := context.WithCancel(rootCtx)
	doneB := make(chan error, 1)
	go func() { doneB <- newController("controller-b", leaderB).Run(ctxB) }()
	select {
	case <-leaderB:
		t.Fatal("controller-a 存活时 controller-b 不得同时成为 leader")
	case <-time.After(150 * time.Millisecond):
	}
	waitAssignedReplicas(t, buckets.Assignments, deployment, 1)
	cancelA()
	select {
	case <-doneA:
	case <-time.After(2 * time.Second):
		t.Fatal("controller-a 取消后未释放领导权")
	}
	select {
	case <-leaderB:
	case <-time.After(2 * time.Second):
		t.Fatal("controller-b 未在 leader 退出后接管")
	}
	cancelB()
	select {
	case <-doneB:
	case <-time.After(2 * time.Second):
		t.Fatal("controller-b 未响应取消")
	}
}

func testDeployment(replicas int) deploymentv2.Deployment {
	return deploymentv2.Deployment{
		Version: 2, Revision: 1, Metadata: deploymentv1.Metadata{Name: "prod", Tenant: "acme"},
		Units: []deploymentv2.ServiceUnit{{
			ID: "api", Kind: "service", Enabled: true, ServiceRole: "backend", Replicas: replicas,
			Placement: deploymentv1.Placement{NodeSelector: map[string]string{"region": "cn"}},
			Plugins:   []deploymentv1.PluginRef{{ID: "com.example.api", Version: "1.0.0", Channel: "stable"}},
		}},
	}
}

func startSchedulerNATS(t *testing.T) (*natsserver.Server, controlplane.Buckets) {
	t.Helper()
	server, err := natsserver.NewServer(&natsserver.Options{
		JetStream: true, StoreDir: filepath.Join(t.TempDir(), "jetstream"),
		Host: "127.0.0.1", Port: -1, NoLog: true, NoSigs: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	go server.Start()
	if !server.ReadyForConnections(10 * time.Second) {
		t.Fatal("嵌入式 NATS 未就绪")
	}
	if _, ok := server.Addr().(*net.TCPAddr); !ok {
		t.Fatal("嵌入式 NATS 未监听 TCP")
	}
	t.Cleanup(func() {
		server.Shutdown()
		server.WaitForShutdown()
	})
	nc, err := nats.Connect(server.ClientURL())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(nc.Close)
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatal(err)
	}
	buckets, err := controlplane.EnsureBuckets(context.Background(), js, 1, jetstream.MemoryStorage)
	if err != nil {
		t.Fatal(err)
	}
	return server, buckets
}

func countUnit(assignments map[string]deploymentv1.DesiredState, unitID string) int {
	count := 0
	for _, assignment := range assignments {
		for _, unit := range assignment.Units {
			if unit.ID == unitID {
				count++
			}
		}
	}
	return count
}

func selectedNode(assignments map[string]deploymentv1.DesiredState, unitID string) string {
	for nodeID, assignment := range assignments {
		for _, unit := range assignment.Units {
			if unit.ID == unitID {
				return nodeID
			}
		}
	}
	return ""
}

func loadAssignment(t *testing.T, kv jetstream.KeyValue, deployment deploymentv2.Deployment, nodeID string) deploymentv1.DesiredState {
	t.Helper()
	entry, err := kv.Get(context.Background(), controlplane.AssignmentKey(deployment.Metadata.Tenant, deployment.Metadata.Name, nodeID))
	if err != nil {
		t.Fatal(err)
	}
	state, err := deploymentv1.Parse(entry.Value())
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func waitAssignedReplicas(t *testing.T, kv jetstream.KeyValue, deployment deploymentv2.Deployment, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		count := 0
		for _, nodeID := range []string{"node-a", "node-b"} {
			entry, err := kv.Get(context.Background(), controlplane.AssignmentKey(deployment.Metadata.Tenant, deployment.Metadata.Name, nodeID))
			if err != nil {
				continue
			}
			state, err := deploymentv1.Parse(entry.Value())
			if err == nil {
				count += len(state.Units)
			}
		}
		if count == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("等待 assignment replicas=%d 超时", want)
}
