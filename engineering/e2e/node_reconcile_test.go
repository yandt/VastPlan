//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	deploymentv1 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v1"
	deploymentv2 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v2"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/deploymentcontroller"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/nodeagent"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/pluginservice"
	"cdsoft.com.cn/VastPlan/core/shared/go/addressing"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/controlplane"
	"cdsoft.com.cn/VastPlan/core/shared/go/protocolbus"
)

type clusterNode struct {
	id         string
	conn       *nats.Conn
	router     *addressing.Router
	runtime    *nodeagent.ProtocolRuntime
	reconciler *nodeagent.Reconciler
	store      *nodeagent.MemoryStateStore
	lease      *controlplane.NodeLease
	cancel     context.CancelFunc
	done       chan error
}

func TestNodeAgent_ThreeNodeReplicaPlacementAndDriftRecovery(t *testing.T) {
	repository, err := pluginservice.NewRepository(filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		t.Fatal(err)
	}
	publishBuiltPlugin(t, repository, "./extensions/plugins/cn.vastplan.demo-permission/backend", "extensions/plugins/cn.vastplan.demo-permission/vastplan.plugin.json")
	publishBuiltPlugin(t, repository, "./extensions/plugins/cn.vastplan.hello-world/backend", "extensions/plugins/cn.vastplan.hello-world/vastplan.plugin.json")
	server := startE2ENATS(t)
	admin, err := nats.Connect(server.ClientURL())
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	js, err := jetstream.New(admin)
	if err != nil {
		t.Fatal(err)
	}
	buckets, err := controlplane.EnsureBuckets(context.Background(), js, 1, jetstream.MemoryStorage)
	if err != nil {
		t.Fatal(err)
	}

	nodes := map[string]*clusterNode{}
	for _, nodeID := range []string{"node-a", "node-b", "node-c"} {
		node := startClusterNode(t, server, buckets, repository, nodeID, filepath.Join(t.TempDir(), nodeID))
		nodes[nodeID] = node
	}
	defer func() {
		for _, node := range nodes {
			node.stop(t)
		}
	}()
	callerConn, err := nats.Connect(server.ClientURL())
	if err != nil {
		t.Fatal(err)
	}
	defer callerConn.Close()
	caller, err := addressing.NewRouter(callerConn, buckets.Capabilities, "caller", t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	defer caller.Close()

	deployment := deploymentv2.Deployment{
		Version: 2, Revision: 1, Metadata: deploymentv1.Metadata{Name: "mesh", Tenant: "acme"},
		Units: []deploymentv2.ServiceUnit{{
			ID: "backend-api", Kind: "service", Enabled: true, ServiceRole: "backend", Replicas: 2,
			Placement: deploymentv2.Placement{NodeSelector: map[string]string{"region": "cn"}},
			Plugins: []deploymentv1.PluginRef{
				{ID: "cn.vastplan.demo-permission", Version: "0.1.0", Channel: "stable"},
				{ID: "cn.vastplan.hello-world", Version: "0.1.0", Channel: "stable"},
			},
		}},
	}
	scheduler := deploymentcontroller.Scheduler{
		Nodes: buckets.Nodes, Assignments: buckets.Assignments, Actual: buckets.Actual, Compositions: buckets.Compositions,
	}
	plan, err := scheduler.Reconcile(context.Background(), deployment)
	if err != nil {
		t.Fatal(err)
	}
	selected := selectedClusterNodes(plan.Assignments, "backend-api")
	if len(selected) != 2 {
		t.Fatalf("初始副本数错误: %v", selected)
	}
	for nodeID, node := range nodes {
		want := 0
		if selected[nodeID] {
			want = 1
		}
		waitMemoryUnits(t, node.store, plan.Generation, want)
	}
	waitAddressingInstances(t, caller, "vastplan.hello", 2)
	composition, err := scheduler.ObserveComposition(context.Background(), deployment)
	if err != nil || composition.Status != deploymentcontroller.CompositionReady || composition.Generation != plan.Generation {
		t.Fatalf("初始组合状态未收敛: report=%+v err=%v", composition, err)
	}

	failedID := ""
	for nodeID := range selected {
		failedID = nodeID
		break
	}
	failed := nodes[failedID]
	failed.stop(t)
	delete(nodes, failedID)
	rescheduled, err := scheduler.Reconcile(context.Background(), deployment)
	if err != nil {
		t.Fatal(err)
	}
	if rescheduled.Generation <= plan.Generation {
		t.Fatalf("节点漂移未推进 generation: before=%d after=%d", plan.Generation, rescheduled.Generation)
	}
	newSelected := selectedClusterNodes(rescheduled.Assignments, "backend-api")
	if len(newSelected) != 2 || newSelected[failedID] {
		t.Fatalf("故障节点仍在调度结果中: failed=%s selected=%v", failedID, newSelected)
	}
	for nodeID, node := range nodes {
		want := 0
		if newSelected[nodeID] {
			want = 1
		}
		waitMemoryUnits(t, node.store, rescheduled.Generation, want)
	}
	waitAddressingInstances(t, caller, "vastplan.hello", 2)
	composition, err = scheduler.ObserveComposition(context.Background(), deployment)
	if err != nil || composition.Status != deploymentcontroller.CompositionReady || composition.Generation != rescheduled.Generation {
		t.Fatalf("故障漂移后组合状态未恢复: report=%+v err=%v", composition, err)
	}
	result, payload, err := caller.Invoke(context.Background(), toolTarget("vastplan.hello", "greet"), testCallContext(), []byte(`{"name":"replica recovery"}`))
	if err != nil || result.GetStatus() != contractv1.CallResult_STATUS_OK || !strings.Contains(string(payload), "replica recovery") {
		t.Fatalf("漂移后 queue group 不可调用: result=%+v payload=%s err=%v", result, payload, err)
	}
}

func TestNodeAgent_RuntimePublishesRealPluginToNATSMesh(t *testing.T) {
	server := startE2ENATS(t)
	admin, err := nats.Connect(server.ClientURL())
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	js, err := jetstream.New(admin)
	if err != nil {
		t.Fatal(err)
	}
	buckets, err := controlplane.EnsureBuckets(context.Background(), js, 1, jetstream.MemoryStorage)
	if err != nil {
		t.Fatal(err)
	}
	callerConn, err := nats.Connect(server.ClientURL())
	if err != nil {
		t.Fatal(err)
	}
	defer callerConn.Close()
	workerConn, err := nats.Connect(server.ClientURL())
	if err != nil {
		t.Fatal(err)
	}
	defer workerConn.Close()
	caller, err := addressing.NewRouter(callerConn, buckets.Capabilities, "node-a", t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	defer caller.Close()
	worker, err := addressing.NewRouter(workerConn, buckets.Capabilities, "node-b", t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Close()

	runtime := nodeagent.NewProtocolRuntime("0.1.0", t.Logf)
	defer runtime.Close()
	if err := runtime.AttachRouter(worker); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Apply(context.Background(), nodeagent.RuntimeUnit{
		ID: "backend-worker", Fingerprint: "mesh-e2e", ServiceRole: "backend",
		Plugins: []nodeagent.InstalledPlugin{
			{ID: "cn.vastplan.demo-permission", Version: "0.1.0", EntryPath: buildPlugin(t, "./extensions/plugins/cn.vastplan.demo-permission/backend")},
			{ID: "cn.vastplan.hello-world", Version: "0.1.0", EntryPath: buildPlugin(t, "./extensions/plugins/cn.vastplan.hello-world/backend")},
		},
	}); err != nil {
		t.Fatal(err)
	}
	waitAddressingInstances(t, caller, "vastplan.hello", 1)
	response, payload, err := caller.Invoke(context.Background(), toolTarget("vastplan.hello", "greet"), testCallContext(), []byte(`{"name":"NATS mesh"}`))
	if err != nil || response.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("跨节点调用真实插件失败: result=%+v payload=%s err=%v", response, payload, err)
	}
	if !strings.Contains(string(payload), "NATS mesh") {
		t.Fatalf("跨节点插件响应内容不匹配: %s", payload)
	}
}

type staticDesiredSource struct {
	state deploymentv1.DesiredState
}

func (s staticDesiredSource) Load(context.Context) (deploymentv1.DesiredState, error) {
	return s.state, nil
}

func TestNodeAgent_RealProcessIdempotencyFailureAndRollback(t *testing.T) {
	repository, err := pluginservice.NewRepository(filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		t.Fatal(err)
	}
	publishBuiltPlugin(t, repository, "./extensions/plugins/cn.vastplan.demo-permission/backend", "extensions/plugins/cn.vastplan.demo-permission/vastplan.plugin.json")
	publishBuiltPlugin(t, repository, "./extensions/plugins/cn.vastplan.hello-world/backend", "extensions/plugins/cn.vastplan.hello-world/vastplan.plugin.json")
	publishBrokenPlugin(t, repository)

	runtime := nodeagent.NewProtocolRuntime("0.1.0", func(format string, args ...any) { t.Logf("[runtime] "+format, args...) })
	t.Cleanup(func() { _ = runtime.Close() })
	reconciler := &nodeagent.Reconciler{
		NodeID: "e2e-node", Sources: []nodeagent.ArtifactSource{repository}, Verifier: nodeagent.NewLocalDevelopmentArtifactVerifier(),
		Installer: nodeagent.LocalInstaller{Root: filepath.Join(t.TempDir(), "installed")},
		Runtime:   runtime, StateStore: nodeagent.NewMemoryStateStore(),
	}
	baseline := deploymentv1.DesiredState{
		Version: 1, Revision: 1, Metadata: deploymentv1.Metadata{Name: "e2e"},
		Units: []deploymentv1.Unit{{
			ID: "backend-main", Kind: "service", Enabled: true, ServiceRole: "backend", Replicas: 1,
			Plugins: []deploymentv1.PluginRef{
				{ID: "cn.vastplan.demo-permission", Version: "0.1.0", Channel: "stable"},
				{ID: "cn.vastplan.hello-world", Version: "0.1.0", Channel: "stable"},
			},
		}},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	first, err := reconciler.Reconcile(ctx, baseline)
	if err != nil || !first.Changed || !first.Converged {
		t.Fatalf("首次真实进程装配失败: result=%+v err=%v", first, err)
	}
	host, ok := runtime.Host("backend-main")
	if !ok {
		t.Fatal("装配后缺少 backend-main 宿主")
	}
	resp, err := host.Invoke(ctx, toolTarget("vastplan.hello", "greet"), testCallContext(), []byte(`{"name":"Node Agent"}`))
	if err != nil || resp.Result.Status != contractv1.CallResult_STATUS_OK {
		t.Fatalf("自动装配贡献不可调用: response=%+v err=%v", resp, err)
	}

	second, err := reconciler.Reconcile(ctx, baseline)
	sameHost, _ := runtime.Host("backend-main")
	if err != nil || second.Changed || sameHost != host {
		t.Fatalf("相同期望态不应重启真实进程: result=%+v sameHost=%v err=%v", second, sameHost == host, err)
	}

	broken := deploymentv1.DesiredState{
		Version: 1, Revision: 2, Metadata: deploymentv1.Metadata{Name: "e2e"},
		Units: []deploymentv1.Unit{{
			ID: "backend-main", Kind: "service", Enabled: true, ServiceRole: "backend", Replicas: 1,
			Plugins: []deploymentv1.PluginRef{{ID: "cn.vastplan.broken", Version: "2.0.0", Channel: "stable"}},
		}},
	}
	failed, err := reconciler.Reconcile(ctx, broken)
	stillOld, _ := runtime.Host("backend-main")
	if err == nil || failed.Converged || stillOld != host {
		t.Fatalf("候选失败后旧宿主必须保留: result=%+v sameHost=%v err=%v", failed, stillOld == host, err)
	}
	resp, err = stillOld.Invoke(ctx, toolTarget("vastplan.hello", "greet"), testCallContext(), []byte(`{"name":"Rollback"}`))
	if err != nil || resp.Result.Status != contractv1.CallResult_STATUS_OK {
		t.Fatalf("失败升级破坏了旧实例: response=%+v err=%v", resp, err)
	}

	rolledBack, err := reconciler.Reconcile(ctx, baseline)
	if err != nil || !rolledBack.Converged || rolledBack.Changed {
		t.Fatalf("回滚到仍运行的旧组合应原地收敛: result=%+v err=%v", rolledBack, err)
	}
	baseline.Revision = 3
	baseline.Units[0].Enabled = false
	stopped, err := reconciler.Reconcile(ctx, baseline)
	if _, ok := runtime.Host("backend-main"); err != nil || !stopped.Converged || !stopped.Changed || ok {
		t.Fatalf("禁用没有停止真实 unit: result=%+v running=%v err=%v", stopped, ok, err)
	}
}

func TestNodeAgent_ProcessCrashTriggersImmediateRecovery(t *testing.T) {
	repository, err := pluginservice.NewRepository(filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		t.Fatal(err)
	}
	publishBuiltPlugin(t, repository, "./extensions/plugins/cn.vastplan.demo-permission/backend", "extensions/plugins/cn.vastplan.demo-permission/vastplan.plugin.json")
	publishCrasherPlugin(t, repository)
	desired := deploymentv1.DesiredState{
		Version: 1, Revision: 1, Metadata: deploymentv1.Metadata{Name: "crash-recovery"},
		Units: []deploymentv1.Unit{{
			ID: "backend-main", Kind: "service", Enabled: true, ServiceRole: "backend", Replicas: 1,
			Plugins: []deploymentv1.PluginRef{
				{ID: "cn.vastplan.demo-permission", Version: "0.1.0", Channel: "stable"},
				{ID: "cn.vastplan.fixture.crasher", Version: "0.1.0", Channel: "stable"},
			},
		}},
	}
	runtime := nodeagent.NewProtocolRuntime("0.1.0", func(format string, args ...any) { t.Logf("[runtime] "+format, args...) })
	store := nodeagent.NewMemoryStateStore()
	reconciler := &nodeagent.Reconciler{
		NodeID: "recovery-node", Sources: []nodeagent.ArtifactSource{repository}, Verifier: nodeagent.NewLocalDevelopmentArtifactVerifier(),
		Installer: nodeagent.LocalInstaller{Root: filepath.Join(t.TempDir(), "installed")},
		Runtime:   runtime, StateStore: store,
	}
	agent := &nodeagent.Agent{
		Source: staticDesiredSource{state: desired}, Reconciler: reconciler,
		Interval: time.Hour, RetryMin: 20 * time.Millisecond, RetryMax: 100 * time.Millisecond,
		Jitter: func(delay time.Duration) time.Duration { return delay },
		Logf:   func(format string, args ...any) { t.Logf("[agent] "+format, args...) },
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- agent.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		<-done
		_ = runtime.Close()
	})

	fingerprint := desired.Units[0].Fingerprint()
	oldHost := waitForRuntimeHost(t, runtime, nil, fingerprint)
	invokeCtx, invokeCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer invokeCancel()
	_, err = oldHost.Invoke(invokeCtx, toolTarget("fixture.crasher", "crash"), testCallContext(), nil)
	if err == nil {
		t.Fatal("crash 调用应因插件失联而失败")
	}
	newHost := waitForRuntimeHost(t, runtime, oldHost, fingerprint)
	resp, err := newHost.Invoke(invokeCtx, toolTarget("fixture.crasher", "ping"), testCallContext(), nil)
	if err != nil || resp.Result.Status != contractv1.CallResult_STATUS_OK {
		t.Fatalf("自动恢复后的插件不可调用: response=%+v err=%v", resp, err)
	}
	actual, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if state := actual.Units["backend-main"]; state.RestartCount != 1 || state.Phase != nodeagent.PhaseActive || len(state.PIDs) != 2 {
		t.Fatalf("恢复后的实际态不完整: %+v", state)
	}
}

func TestNodeAgent_NATSKVWatchDrivesRealUnitAndReportsActualState(t *testing.T) {
	ns := startE2ENATS(t)
	nc, err := controlplane.Connect(ns.ClientURL(), "nodeagent-e2e", t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(nc.Close)
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	buckets, err := controlplane.EnsureBuckets(ctx, js, 1, jetstream.MemoryStorage)
	if err != nil {
		t.Fatal(err)
	}

	repository, err := pluginservice.NewRepository(filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		t.Fatal(err)
	}
	publishBuiltPlugin(t, repository, "./extensions/plugins/cn.vastplan.demo-permission/backend", "extensions/plugins/cn.vastplan.demo-permission/vastplan.plugin.json")
	publishBuiltPlugin(t, repository, "./extensions/plugins/cn.vastplan.hello-world/backend", "extensions/plugins/cn.vastplan.hello-world/vastplan.plugin.json")
	desired := deploymentv1.DesiredState{
		Version: 1, Revision: 1, Metadata: deploymentv1.Metadata{Name: "nats-e2e"},
		Units: []deploymentv1.Unit{{
			ID: "backend-main", Kind: "service", Enabled: true, ServiceRole: "backend", Replicas: 1,
			Plugins: []deploymentv1.PluginRef{
				{ID: "cn.vastplan.demo-permission", Version: "0.1.0", Channel: "stable"},
				{ID: "cn.vastplan.hello-world", Version: "0.1.0", Channel: "stable"},
			},
		}},
	}
	desiredKey := controlplane.DesiredKey("", desired.Metadata.Name)
	applyDesiredE2E(t, ctx, buckets.Desired, desiredKey, desired)

	runtime := nodeagent.NewProtocolRuntime("0.1.0", t.Logf)
	localStore := nodeagent.FileStateStore{Path: filepath.Join(t.TempDir(), "actual.json")}
	remoteStore := nodeagent.NATSStateStore{KV: buckets.Actual, Key: controlplane.ActualKey("_global", "nats-e2e", "nats-node")}
	reconciler := &nodeagent.Reconciler{
		NodeID: "nats-node", Sources: []nodeagent.ArtifactSource{repository}, Verifier: nodeagent.NewLocalDevelopmentArtifactVerifier(),
		Installer: nodeagent.LocalInstaller{Root: filepath.Join(t.TempDir(), "installed")}, Runtime: runtime,
		StateStore: nodeagent.ReplicatedStateStore{Primary: localStore, Replicas: []nodeagent.StateStore{remoteStore}},
	}
	agent := &nodeagent.Agent{
		Source:     nodeagent.NATSDesiredStateSource{KV: buckets.Desired, Key: desiredKey, Conn: nc},
		Reconciler: reconciler, Interval: time.Hour,
		RetryMin: 20 * time.Millisecond, RetryMax: 100 * time.Millisecond,
		Jitter: func(delay time.Duration) time.Duration { return delay }, Logf: t.Logf,
	}
	agentCtx, agentCancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- agent.Run(agentCtx) }()
	t.Cleanup(func() {
		agentCancel()
		<-done
		_ = runtime.Close()
	})
	normalizedRaw, err := json.Marshal(desired)
	if err != nil {
		t.Fatal(err)
	}
	parsedDesired, err := deploymentv1.Parse(normalizedRaw)
	if err != nil {
		t.Fatal(err)
	}
	waitForRuntimeHost(t, runtime, nil, parsedDesired.Units[0].Fingerprint())
	waitActualRevision(t, remoteStore, 1, 1)

	desired.Revision = 2
	desired.Units[0].Enabled = false
	applyDesiredE2E(t, ctx, buckets.Desired, desiredKey, desired)
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := runtime.Host("backend-main"); !ok {
			waitActualRevision(t, remoteStore, 2, 0)
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("NATS DesiredState 禁用后 unit 未停止")
}

func waitForRuntimeHost(t *testing.T, runtime *nodeagent.ProtocolRuntime, previous *protocolbus.Host, fingerprint string) *protocolbus.Host {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		host, ok := runtime.Host("backend-main")
		if ok && runtime.IsRunning("backend-main", fingerprint) && (previous == nil || host != previous) {
			return host
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("等待 backend-main 健康宿主超时")
	return nil
}

func startE2ENATS(t *testing.T) *natsserver.Server {
	t.Helper()
	ns, err := natsserver.NewServer(&natsserver.Options{
		JetStream: true, StoreDir: filepath.Join(t.TempDir(), "jetstream"),
		Host: "127.0.0.1", Port: -1, NoLog: true, NoSigs: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	go ns.Start()
	if !ns.ReadyForConnections(10 * time.Second) {
		t.Fatal("嵌入式 NATS 未就绪")
	}
	if _, ok := ns.Addr().(*net.TCPAddr); !ok {
		t.Fatal("嵌入式 NATS 未监听 TCP")
	}
	t.Cleanup(func() {
		ns.Shutdown()
		ns.WaitForShutdown()
	})
	return ns
}

func applyDesiredE2E(t *testing.T, ctx context.Context, kv jetstream.KeyValue, key string, desired deploymentv1.DesiredState) {
	t.Helper()
	raw, err := json.Marshal(desired)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := controlplane.ApplyDesiredState(ctx, kv, key, raw); err != nil {
		t.Fatal(err)
	}
}

func waitActualRevision(t *testing.T, store nodeagent.NATSStateStore, revision uint64, units int) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		actual, err := store.Load()
		if err == nil && actual.AppliedRevision == revision && len(actual.Units) == units {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	actual, err := store.Load()
	t.Fatalf("等待远端实际态 revision=%d units=%d 超时: actual=%+v err=%v", revision, units, actual, err)
}

func waitAddressingInstances(t *testing.T, router *addressing.Router, capability string, want int) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if len(router.Instances(capability)) == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("等待 capability=%s instances=%d 超时，当前=%d", capability, want, len(router.Instances(capability)))
}

func startClusterNode(t *testing.T, server *natsserver.Server, buckets controlplane.Buckets, repository *pluginservice.Repository, nodeID, runtimeRoot string) *clusterNode {
	t.Helper()
	conn, err := nats.Connect(server.ClientURL())
	if err != nil {
		t.Fatal(err)
	}
	router, err := addressing.NewRouter(conn, buckets.Capabilities, nodeID, t.Logf)
	if err != nil {
		conn.Close()
		t.Fatal(err)
	}
	runtime := nodeagent.NewProtocolRuntime("0.1.0", t.Logf)
	if err := runtime.AttachRouter(router); err != nil {
		_ = router.Close()
		conn.Close()
		t.Fatal(err)
	}
	store := nodeagent.NewMemoryStateStore()
	reconciler := &nodeagent.Reconciler{
		NodeID: nodeID, NodeLabels: map[string]string{"region": "cn"},
		Sources: []nodeagent.ArtifactSource{repository}, Verifier: nodeagent.NewLocalDevelopmentArtifactVerifier(),
		Installer: nodeagent.LocalInstaller{Root: runtimeRoot}, Runtime: runtime,
		StateStore: nodeagent.ReplicatedStateStore{Primary: store, Replicas: []nodeagent.StateStore{
			nodeagent.NATSStateStore{KV: buckets.Actual, Key: controlplane.ActualKey("acme", "mesh", nodeID)},
		}},
	}
	ctx, cancel := context.WithCancel(context.Background())
	lease, err := controlplane.StartNodeLease(ctx, buckets.Nodes, nodeID, map[string]string{"region": "cn"}, controlplane.NodeLeaseOptions{TenantID: "acme", Deployment: "mesh", AllowUnattested: true})
	if err != nil {
		cancel()
		_ = runtime.Close()
		_ = router.Close()
		conn.Close()
		t.Fatal(err)
	}
	agent := &nodeagent.Agent{
		Source: nodeagent.NATSDesiredStateSource{
			KV: buckets.Assignments, Key: controlplane.AssignmentKey("acme", "mesh", nodeID), Conn: conn,
		},
		Reconciler: reconciler, Interval: time.Hour, RetryMin: 20 * time.Millisecond, RetryMax: 100 * time.Millisecond, Logf: t.Logf,
	}
	node := &clusterNode{
		id: nodeID, conn: conn, router: router, runtime: runtime, reconciler: reconciler,
		store: store, lease: lease, cancel: cancel, done: make(chan error, 1),
	}
	go func() { node.done <- agent.Run(ctx) }()
	return node
}

func (node *clusterNode) stop(t *testing.T) {
	t.Helper()
	if node == nil || node.cancel == nil {
		return
	}
	node.cancel()
	select {
	case <-node.done:
	case <-time.After(5 * time.Second):
		t.Errorf("等待节点 %s Agent 退出超时", node.id)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if err := node.lease.Close(ctx); err != nil {
		t.Errorf("关闭节点 %s 租约: %v", node.id, err)
	}
	if err := node.reconciler.Shutdown(ctx); err != nil {
		t.Errorf("关闭节点 %s unit: %v", node.id, err)
	}
	_ = node.runtime.Close()
	_ = node.router.Close()
	node.conn.Close()
	node.cancel = nil
}

func selectedClusterNodes(assignments map[string]deploymentv1.DesiredState, unitID string) map[string]bool {
	selected := map[string]bool{}
	for nodeID, assignment := range assignments {
		for _, unit := range assignment.Units {
			if unit.ID == unitID {
				selected[nodeID] = true
			}
		}
	}
	return selected
}

func waitMemoryUnits(t *testing.T, store *nodeagent.MemoryStateStore, revision uint64, units int) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		actual, err := store.Load()
		if err == nil && actual.AppliedRevision == revision && len(actual.Units) == units {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	actual, err := store.Load()
	t.Fatalf("等待内存实际态 revision=%d units=%d 超时: actual=%+v err=%v", revision, units, actual, err)
}

func publishBuiltPlugin(t *testing.T, repository *pluginservice.Repository, packageDir, manifestPath string) {
	t.Helper()
	bin := buildPlugin(t, packageDir)
	manifestRaw, err := os.ReadFile(filepath.Join(repoRoot(t), manifestPath))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := pluginv1.ParseManifest(manifestRaw)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "vastplan.plugin.json"), manifestRaw, 0o644); err != nil {
		t.Fatal(err)
	}
	if manifest.License != "" {
		licenseRaw, err := os.ReadFile(filepath.Join(repoRoot(t), "LICENSE"))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(manifest.LicenseFile)), licenseRaw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if manifest.NoticeFile != "" {
		noticeRaw, err := os.ReadFile(filepath.Join(repoRoot(t), "NOTICE"))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(manifest.NoticeFile)), noticeRaw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	entry := filepath.Join(dir, filepath.FromSlash(manifest.Entry["backend"]))
	if err := os.MkdirAll(filepath.Dir(entry), 0o755); err != nil {
		t.Fatal(err)
	}
	binBytes, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(entry, binBytes, 0o755); err != nil {
		t.Fatal(err)
	}
	if frontendEntry := manifest.Entry["frontend"]; frontendEntry != "" {
		frontendPath := filepath.Join(dir, filepath.FromSlash(frontendEntry))
		if err := os.MkdirAll(filepath.Dir(frontendPath), 0o755); err != nil {
			t.Fatal(err)
		}
		module := []byte(`export default { register(context) { context.addRoute({ path: "/settings/portals", component: () => null }); context.addMenu({ id: "portal-composer", title: "Portal", route: "/settings/portals" }); } };`)
		if err := os.WriteFile(frontendPath, module, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	packageBytes, _, err := pluginservice.PackageDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Publish("stable", packageBytes); err != nil {
		t.Fatal(err)
	}
}

func publishBrokenPlugin(t *testing.T, repository *pluginservice.Repository) {
	t.Helper()
	dir := t.TempDir()
	manifest := []byte(`{
  "id":"cn.vastplan.broken","name":"broken","description":"broken candidate",
  "version":"2.0.0","publisher":"vastplan","engines":{"backend":"^0.1"},
  "activation":["onStartup"],"entry":{"backend":"backend/broken"},
  "contributes":{"backend":{"tools":[{"id":"broken.tool","service_role":"backend"}]}}
}`)
	if err := os.WriteFile(filepath.Join(dir, "vastplan.plugin.json"), manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "backend"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "backend", "broken"), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	packageBytes, _, err := pluginservice.PackageDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Publish("stable", packageBytes); err != nil {
		t.Fatal(err)
	}
}

func publishCrasherPlugin(t *testing.T, repository *pluginservice.Repository) {
	t.Helper()
	bin := buildPlugin(t, "./engineering/e2e/fixtures/plugins/crasher")
	dir := t.TempDir()
	manifest := []byte(`{
  "id":"cn.vastplan.fixture.crasher","name":"crasher","description":"crash recovery fixture",
  "version":"0.1.0","publisher":"vastplan","engines":{"backend":"^0.1"},
  "activation":["onStartup"],"entry":{"backend":"backend/crasher"},
  "contributes":{"backend":{"tools":[{"id":"fixture.crasher","service_role":"backend","title":"故意崩溃的夹具"}]}}
}`)
	if err := os.WriteFile(filepath.Join(dir, "vastplan.plugin.json"), manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "backend"), 0o755); err != nil {
		t.Fatal(err)
	}
	binBytes, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "backend", "crasher"), binBytes, 0o755); err != nil {
		t.Fatal(err)
	}
	packageBytes, _, err := pluginservice.PackageDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Publish("stable", packageBytes); err != nil {
		t.Fatal(err)
	}
}
