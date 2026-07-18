package deploymentcontroller

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	deploymentv1 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v1"
	deploymentv2 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v2"
	"cdsoft.com.cn/VastPlan/core/shared/go/controlplane"
)

func TestSchedulerReplicasPlacementScaleAndNodeLoss(t *testing.T) {
	_, buckets := startSchedulerNATS(t)
	ctx := context.Background()
	leases := map[string]*controlplane.NodeLease{}
	for nodeID, labels := range map[string]map[string]string{
		"node-a": {"region": "cn"}, "node-b": {"region": "cn"}, "node-c": {"region": "us"},
	} {
		lease, err := controlplane.StartNodeLease(ctx, buckets.Nodes, nodeID, labels, testNodeLeaseOptions())
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

func TestSchedulerDoesNotTurnAppProfilesIntoServiceAssignments(t *testing.T) {
	_, buckets := startSchedulerNATS(t)
	ctx := context.Background()
	options := testNodeLeaseOptions()
	options.TenantID = "tenant-a"
	options.Deployment = "runner-fleet"
	lease, err := controlplane.StartNodeLease(ctx, buckets.Nodes, "node-a", map[string]string{"region": "cn"}, options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lease.Close(context.Background()) })

	deployment := deploymentv2.Deployment{
		Version: 2, Revision: 1,
		Metadata:    deploymentv1.Metadata{Name: "runner-fleet", Tenant: "tenant-a"},
		Units:       []deploymentv2.ServiceUnit{},
		AppProfiles: []deploymentv2.AppProfileRef{{ID: "collector", Revision: 3, Digest: strings.Repeat("a", 64)}},
	}
	plan, err := (Scheduler{Nodes: buckets.Nodes, Assignments: buckets.Assignments}).Reconcile(ctx, deployment)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Assignments["node-a"].Units) != 0 {
		t.Fatalf("App Profile 不得进入 ServiceUnit assignment: %+v", plan.Assignments["node-a"])
	}
}

func TestSchedulerUsesOnlyMatchingTenantAndDeploymentLeases(t *testing.T) {
	_, buckets := startSchedulerNATS(t)
	ctx := context.Background()
	matching, err := controlplane.StartNodeLease(ctx, buckets.Nodes, "node-a", map[string]string{"region": "cn"}, testNodeLeaseOptions())
	if err != nil {
		t.Fatal(err)
	}
	otherOptions := testNodeLeaseOptions()
	otherOptions.TenantID = "other"
	other, err := controlplane.StartNodeLease(ctx, buckets.Nodes, "node-b", map[string]string{"region": "cn"}, otherOptions)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = matching.Close(context.Background()); _ = other.Close(context.Background()) })
	plan, err := (Scheduler{Nodes: buckets.Nodes, Assignments: buckets.Assignments}).Reconcile(ctx, testDeployment(1))
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := plan.Assignments["node-b"]; exists {
		t.Fatal("其他租户的活跃节点不得进入当前 deployment 调度候选")
	}
}

func TestControllerWatchesDeploymentUpdates(t *testing.T) {
	_, buckets := startSchedulerNATS(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for nodeID := range map[string]struct{}{"node-a": {}, "node-b": {}} {
		lease, err := controlplane.StartNodeLease(ctx, buckets.Nodes, nodeID, map[string]string{"region": "cn"}, testNodeLeaseOptions())
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
	lease, err := controlplane.StartNodeLease(rootCtx, buckets.Nodes, "node-a", map[string]string{"region": "cn"}, testNodeLeaseOptions())
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

func TestSchedulerResourceAccountingAndAffinity(t *testing.T) {
	_, buckets := startSchedulerNATS(t)
	ctx := context.Background()
	leases := []*controlplane.NodeLease{}
	nodes := []struct {
		id       string
		labels   map[string]string
		capacity controlplane.ResourceCapacity
	}{
		{"node-a", map[string]string{"region": "cn", "disk": "hdd"}, controlplane.ResourceCapacity{CPUMillis: 1000, MemoryBytes: 2000}},
		{"node-b", map[string]string{"region": "cn", "disk": "ssd"}, controlplane.ResourceCapacity{CPUMillis: 2000, MemoryBytes: 2000}},
		{"node-c", map[string]string{"region": "cn", "disk": "ssd", "maintenance": "true"}, controlplane.ResourceCapacity{CPUMillis: 4000, MemoryBytes: 4000}},
	}
	for _, node := range nodes {
		options := testNodeLeaseOptions()
		options.Capacity = node.capacity
		lease, err := controlplane.StartNodeLease(ctx, buckets.Nodes, node.id, node.labels, options)
		if err != nil {
			t.Fatal(err)
		}
		leases = append(leases, lease)
	}
	defer func() {
		for _, lease := range leases {
			_ = lease.Close(context.Background())
		}
	}()
	deployment := testDeployment(1)
	deployment.Units[0].Resources.Requests = deploymentv2.ResourceList{CPUMillis: 1500, MemoryBytes: 1000}
	deployment.Units[0].Placement.AntiAffinity.Required = []deploymentv2.LabelTerm{{MatchLabels: map[string]string{"maintenance": "true"}}}
	deployment.Units[0].Placement.Affinity.Preferred = []deploymentv2.WeightedLabelTerm{{MatchLabels: map[string]string{"disk": "ssd"}, Weight: 100}}
	deployment.Units = append(deployment.Units, deploymentv2.ServiceUnit{
		ID: "worker", Kind: "service", Enabled: true, ServiceRole: "backend", Replicas: 1,
		Plugins:   []deploymentv1.PluginRef{{ID: "com.example.worker", Version: "1.0.0", Channel: "stable"}},
		Resources: deploymentv2.ResourceRequirements{Requests: deploymentv2.ResourceList{CPUMillis: 1000, MemoryBytes: 1000}},
		Placement: deploymentv2.Placement{NodeSelector: map[string]string{"region": "cn"}, AntiAffinity: deploymentv2.LabelPolicy{
			Required: []deploymentv2.LabelTerm{{MatchLabels: map[string]string{"maintenance": "true"}}},
		}},
	})
	scheduler := Scheduler{Nodes: buckets.Nodes, Assignments: buckets.Assignments}
	plan, err := scheduler.Reconcile(ctx, deployment)
	if err != nil {
		t.Fatal(err)
	}
	if selectedNode(plan.Assignments, "api") != "node-b" {
		t.Fatalf("资源硬过滤和 SSD 亲和应选择 node-b: %+v", plan.Assignments)
	}
	if selectedNode(plan.Assignments, "worker") != "node-a" {
		t.Fatalf("node-b 剩余 CPU 不足后 worker 应选择 node-a: %+v", plan.Assignments)
	}
	foreign := deploymentv1.DesiredState{
		Version: 1, Revision: 1, Metadata: deploymentv1.Metadata{Name: "foreign", Tenant: "acme"},
		Units: []deploymentv1.Unit{{
			ID: "foreign", Kind: "service", Enabled: true, ServiceRole: "backend", Replicas: 1,
			Plugins:   []deploymentv1.PluginRef{{ID: "com.example.foreign", Version: "1.0.0", Channel: "stable"}},
			Resources: deploymentv1.ResourceRequirements{Requests: deploymentv1.ResourceList{CPUMillis: 600}},
		}},
	}
	foreignRaw, _ := json.Marshal(foreign)
	if _, _, err := controlplane.ApplyDesiredState(ctx, buckets.Assignments, controlplane.AssignmentKey("acme", "foreign", "node-b"), foreignRaw); err != nil {
		t.Fatal(err)
	}
	deployment.Revision++
	if _, err := scheduler.Reconcile(ctx, deployment); err == nil {
		t.Fatal("其他 deployment 已占用 node-b 资源时必须计入容量并 fail-closed")
	}
	if err := buckets.Assignments.Delete(ctx, controlplane.AssignmentKey("acme", "foreign", "node-b")); err != nil {
		t.Fatal(err)
	}

	deployment.Revision++
	deployment.Units[1].Resources.Requests.CPUMillis = 1100
	if _, err := scheduler.Reconcile(ctx, deployment); err == nil {
		t.Fatal("排除维护节点后总容量不足必须 fail-closed")
	}
	kept := loadAssignment(t, buckets.Assignments, deployment, "node-a")
	if countUnit(map[string]deploymentv1.DesiredState{"node-a": kept}, "worker") != 1 {
		t.Fatal("资源不足不得破坏最近健康 assignment")
	}
}

func TestSchedulerAutoscalingMetricBoundsAndMissingMetric(t *testing.T) {
	_, buckets := startSchedulerNATS(t)
	ctx := context.Background()
	leases := []*controlplane.NodeLease{}
	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("node-%d", i)
		lease, err := controlplane.StartNodeLease(ctx, buckets.Nodes, id, map[string]string{"region": "cn"}, testNodeLeaseOptions())
		if err != nil {
			t.Fatal(err)
		}
		leases = append(leases, lease)
	}
	defer func() {
		for _, lease := range leases {
			_ = lease.Close(context.Background())
		}
	}()
	deployment := testDeployment(2)
	deployment.Units[0].Autoscaling = &deploymentv2.Autoscaling{MinReplicas: 2, MaxReplicas: 5, Metric: "queue.depth", TargetValuePerReplica: 10}
	scheduler := Scheduler{Nodes: buckets.Nodes, Assignments: buckets.Assignments, Metrics: buckets.Autoscaling}
	publish := func(value float64) {
		t.Helper()
		if err := controlplane.PublishAutoscalingMetric(ctx, buckets.Autoscaling, controlplane.AutoscalingMetric{
			Tenant: "acme", Deployment: "prod", Unit: "api", Metric: "queue.depth", Value: value,
		}); err != nil {
			t.Fatal(err)
		}
	}
	publish(21)
	plan, err := scheduler.Reconcile(ctx, deployment)
	if err != nil || countUnit(plan.Assignments, "api") != 3 {
		t.Fatalf("21/10 应扩为 3 副本: count=%d err=%v", countUnit(plan.Assignments, "api"), err)
	}
	deployment.Revision++
	publish(100)
	plan, err = scheduler.Reconcile(ctx, deployment)
	if err != nil || countUnit(plan.Assignments, "api") != 5 {
		t.Fatalf("自动伸缩应受 max=5 限制: count=%d err=%v", countUnit(plan.Assignments, "api"), err)
	}
	deployment.Revision++
	publish(0)
	plan, err = scheduler.Reconcile(ctx, deployment)
	if err != nil || countUnit(plan.Assignments, "api") != 2 {
		t.Fatalf("零负载应保持 min=2: count=%d err=%v", countUnit(plan.Assignments, "api"), err)
	}
	deployment.Units[0].Autoscaling.Metric = "missing"
	if _, err := scheduler.Reconcile(ctx, deployment); err == nil {
		t.Fatal("自动伸缩指标缺失必须 fail-closed")
	}
	deployment.Units[0].Autoscaling.Metric = "queue.depth"
	if err := controlplane.PublishAutoscalingMetric(ctx, buckets.Autoscaling, controlplane.AutoscalingMetric{
		Tenant: "acme", Deployment: "prod", Unit: "api", Metric: "queue.depth", Value: 10,
		ObservedAt: time.Now().Add(-controlplane.AutoscalingMetricMaxAge - time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := scheduler.Reconcile(ctx, deployment); err == nil {
		t.Fatal("时间戳过期的自动伸缩指标必须 fail-closed")
	}
}

func TestSchedulerPartitionsAreUniqueAndRebalanceAfterNodeLoss(t *testing.T) {
	_, buckets := startSchedulerNATS(t)
	ctx := context.Background()
	leases := map[string]*controlplane.NodeLease{}
	for _, nodeID := range []string{"node-a", "node-b"} {
		lease, err := controlplane.StartNodeLease(ctx, buckets.Nodes, nodeID, map[string]string{"region": "cn"}, testNodeLeaseOptions())
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
	deployment := testDeployment(2)
	unit := &deployment.Units[0]
	unit.LogicalService = "platform.database"
	unit.InstancePolicy = "partitioned"
	unit.StateModel = "partition-owned"
	unit.Visibility = "cluster"
	unit.Routing = "shard"
	unit.RoutingDomain = "core"
	unit.PartitionKeys = []string{"tenant-a", "tenant-b", "tenant-c", "tenant-d"}
	scheduler := Scheduler{Nodes: buckets.Nodes, Assignments: buckets.Assignments}
	plan, err := scheduler.Reconcile(ctx, deployment)
	if err != nil {
		t.Fatal(err)
	}
	assertPartitionCoverage(t, plan.Assignments, unit.PartitionKeys)
	if countUnit(plan.Assignments, "api") != 2 {
		t.Fatalf("两个 owner 节点都应收到 partitioned unit: %+v", plan.Assignments)
	}
	if err := leases["node-a"].Close(ctx); err != nil {
		t.Fatal(err)
	}
	delete(leases, "node-a")
	rebalanced, err := scheduler.Reconcile(ctx, deployment)
	if err != nil {
		t.Fatal(err)
	}
	assertPartitionCoverage(t, rebalanced.Assignments, unit.PartitionKeys)
	if countUnit(rebalanced.Assignments, "api") != 1 || len(rebalanced.Assignments["node-a"].Units) != 0 {
		t.Fatalf("节点失联后全部分片必须迁往存活 owner: %+v", rebalanced.Assignments)
	}
}

func assertPartitionCoverage(t *testing.T, assignments map[string]deploymentv1.DesiredState, want []string) {
	t.Helper()
	seen := map[string]string{}
	for nodeID, assignment := range assignments {
		for _, unit := range assignment.Units {
			if unit.ID != "api" {
				continue
			}
			raw, ok := unit.Config["partition_keys"].([]any)
			if !ok {
				// 计划写入 KV 前仍保留 []string；JSON round-trip 后是 []any。
				if stringsRaw, stringsOK := unit.Config["partition_keys"].([]string); stringsOK {
					for _, key := range stringsRaw {
						if previous := seen[key]; previous != "" {
							t.Fatalf("分片 %s 同时分配给 %s 和 %s", key, previous, nodeID)
						}
						seen[key] = nodeID
					}
					continue
				}
				t.Fatalf("partition_keys 类型错误: %#v", unit.Config["partition_keys"])
			}
			for _, value := range raw {
				key, _ := value.(string)
				if previous := seen[key]; previous != "" {
					t.Fatalf("分片 %s 同时分配给 %s 和 %s", key, previous, nodeID)
				}
				seen[key] = nodeID
			}
		}
	}
	if len(seen) != len(want) {
		t.Fatalf("分片覆盖不完整: got=%v want=%v", seen, want)
	}
	for _, key := range want {
		if seen[key] == "" {
			t.Fatalf("分片 %s 未分配", key)
		}
	}
}

func testDeployment(replicas int) deploymentv2.Deployment {
	return deploymentv2.Deployment{
		Version: 2, Revision: 1, Metadata: deploymentv1.Metadata{Name: "prod", Tenant: "acme"},
		Resolution: deploymentv2.Resolution{
			PlatformProfile:        deploymentv2.CompositionRef{ID: "test-platform", Revision: 1, Digest: strings.Repeat("a", 64)},
			ApplicationComposition: deploymentv2.CompositionRef{ID: "test-application", Revision: 1, Digest: strings.Repeat("b", 64)},
			PluginOrigins:          map[string]string{"com.example.api": deploymentv2.OriginApplication},
		},
		Units: []deploymentv2.ServiceUnit{{
			ID: "api", Kind: "service", Enabled: true, ServiceRole: "backend", Replicas: replicas,
			Placement: deploymentv2.Placement{NodeSelector: map[string]string{"region": "cn"}},
			Plugins:   []deploymentv1.PluginRef{{ID: "com.example.api", Version: "1.0.0", Channel: "stable"}},
		}},
	}
}

func testNodeLeaseOptions() controlplane.NodeLeaseOptions {
	return controlplane.NodeLeaseOptions{TenantID: "acme", Deployment: "prod", AllowUnattested: true}
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
