//go:build e2e

package e2e

import (
	"context"
	"runtime"
	"sort"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
	deploymentv2 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v2"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/deploymentcontroller"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/nodeagent"
	"cdsoft.com.cn/VastPlan/core/shared/go/addressing"
	"cdsoft.com.cn/VastPlan/core/shared/go/controlplane"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/platformadminapi"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.infrastructure.deployment-manager/deploymentmanager"
)

func TestApplicationIntentP5_ApprovalRevocationPublishAndNodeConvergence(t *testing.T) {
	repository := newP5FixtureRepository(t)
	host := newP5PipelineHost(t, repository)
	manager := deploymentmanager.New()
	request := p5PlanningRequest([]string{"audit"}, p5PlatformProfile())

	var draft platformadminapi.ServiceRevision
	p5RequireOK(t, p5Invoke(t, manager, host, p5UserCall("alice"), "createIntentDraft", map[string]any{"intent": request.Intent}, &draft))
	if draft.Status != platformadminapi.ServiceDraft || draft.ResolutionReport == nil || draft.ResolutionReport.Status != backendcompositionv1.ResolutionNeedsConfiguration {
		t.Fatalf("创建 Intent 草稿没有保留配置缺口: %+v", draft)
	}

	var configured platformadminapi.ServiceRevision
	p5RequireOK(t, p5Invoke(t, manager, host, p5PluginSettingsCall(), "bindIntentConfiguration", map[string]any{
		"revisionId": draft.ID, "configurationSnapshot": *p5CredentialSnapshot(),
	}, &configured))
	if configured.ResolutionReport == nil || configured.ResolutionReport.Status != backendcompositionv1.ResolutionResolved || configured.PreviewDigest == "" {
		t.Fatalf("可信配置绑定后计划未收敛: %+v", configured)
	}

	var submitted platformadminapi.ServiceRevision
	p5RequireOK(t, p5Invoke(t, manager, host, p5UserCall("alice"), "submitServiceDraft", map[string]any{"revisionId": draft.ID}, &submitted))
	if submitted.Status != platformadminapi.ServicePendingApproval || submitted.SubmittedPlanDigest == "" {
		t.Fatalf("Intent 提交状态错误: %+v", submitted)
	}

	repository.publishAuditUpgrade(t)
	result := p5Invoke(t, manager, host, p5UserCall("bob"), "approveServiceRevision", map[string]any{"revisionId": draft.ID}, nil)
	if result.GetStatus() != contractv1.CallResult_STATUS_ERROR || result.GetError().GetCode() != "platform.deployment.plan_stale" {
		t.Fatalf("仓库漂移后旧审批未被撤销: %+v", result)
	}

	var revisions struct {
		Items []platformadminapi.ServiceRevision `json:"items"`
	}
	p5RequireOK(t, p5Invoke(t, manager, host, p5UserCall("alice"), "listServiceRevisions", struct{}{}, &revisions))
	if len(revisions.Items) != 1 || !revisions.Items[0].PlanningStale || revisions.Items[0].Status != platformadminapi.ServiceDraft {
		t.Fatalf("stale 证据没有持久化并退回 Draft: %+v", revisions.Items)
	}

	var refreshed platformadminapi.ServiceRevision
	p5RequireOK(t, p5Invoke(t, manager, host, p5UserCall("alice"), "refreshIntentDraft", map[string]any{"revisionId": draft.ID}, &refreshed))
	if refreshed.PlanningStale || !p5LockContains(refreshed, p5AuditID, "1.2.0") {
		t.Fatalf("显式刷新没有接受最新签名制品: %+v", refreshed)
	}
	p5RequireOK(t, p5Invoke(t, manager, host, p5UserCall("alice"), "submitServiceDraft", map[string]any{"revisionId": draft.ID}, &submitted))
	var approved platformadminapi.ServiceRevision
	p5RequireOK(t, p5Invoke(t, manager, host, p5UserCall("bob"), "approveServiceRevision", map[string]any{"revisionId": draft.ID}, &approved))

	var published platformadminapi.ServiceRevision
	p5RequireOK(t, p5Invoke(t, manager, host, p5UserCall("carol"), "publishServiceRevision", map[string]any{"revisionId": draft.ID}, &published))
	if published.Status != platformadminapi.ServicePublished || !published.Active || published.KVRevision == 0 || published.ReferencePending {
		t.Fatalf("Intent 发布没有形成活动 Revision: %+v", published)
	}
	if published.Preview.Metadata.Tenant != "acme" || published.Preview.Metadata.Name != "p5-pipeline" || len(published.Preview.Units) != 1 {
		t.Fatalf("发布器输出的 Deployment v2 不完整: %+v", published.Preview)
	}
	if catalog := host.catalogs.tenants["acme"]; catalog.Deployment != "p5-pipeline" || catalog.Digest == "" {
		t.Fatalf("发布没有同步可信配置目录: %+v", catalog)
	}

	p5ConvergeSignedDeployment(t, repository, published.Preview)
}

func p5RequireOK(t *testing.T, result *contractv1.CallResult) {
	t.Helper()
	if result == nil || result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("P5 操作失败: %+v", result)
	}
}

func p5LockContains(revision platformadminapi.ServiceRevision, pluginID, version string) bool {
	if revision.ResolutionReport == nil || revision.ResolutionReport.ArtifactLock == nil {
		return false
	}
	for _, item := range revision.ResolutionReport.ArtifactLock.Packages {
		if item.Ref.PluginID == pluginID && item.Ref.Version == version {
			return true
		}
	}
	return false
}

func p5ConvergeSignedDeployment(t *testing.T, repository *p5FixtureRepository, deployment deploymentv2.Deployment) {
	t.Helper()
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
	node := startP5SignedNode(t, server, buckets, repository)
	defer node.stop(t)

	scheduler := deploymentcontroller.Scheduler{
		Nodes: buckets.Nodes, Assignments: buckets.Assignments, Actual: buckets.Actual, Compositions: buckets.Compositions,
	}
	plan, err := scheduler.Reconcile(context.Background(), deployment)
	if err != nil {
		t.Fatal(err)
	}
	waitMemoryUnits(t, node.store, plan.Generation, 1)
	waitCompositionReady(t, scheduler, deployment, plan.Generation)

	actual, err := node.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	unit := actual.Units["pipeline"]
	if unit.Phase != nodeagent.PhaseActive || len(unit.PIDs) != 4 {
		t.Fatalf("真实签名插件组合未在单一 unit 中收敛: phase=%s pids=%v plugins=%v", unit.Phase, unit.PIDs, p5InstalledPluginIDs(unit.Plugins))
	}
	want := []string{p5AuditID, p5CommonID, p5QuotaID, p5RootID}
	if got := p5InstalledPluginIDs(unit.Plugins); !equalStrings(got, want) {
		t.Fatalf("Node Agent 安装闭包错误: got=%v want=%v", got, want)
	}
}

func startP5SignedNode(t *testing.T, server *natsserver.Server, buckets controlplane.Buckets, repository *p5FixtureRepository) *clusterNode {
	t.Helper()
	const nodeID = "p5-node"
	conn, err := nats.Connect(server.ClientURL())
	if err != nil {
		t.Fatal(err)
	}
	router, err := addressing.NewRouter(conn, buckets.Capabilities, nodeID, t.Logf)
	if err != nil {
		conn.Close()
		t.Fatal(err)
	}
	pluginRuntime := nodeagent.NewProtocolRuntime("0.1.0", t.Logf)
	pluginRuntime.Identity = nodeID
	if err := pluginRuntime.AttachRouter(router); err != nil {
		_ = router.Close()
		conn.Close()
		t.Fatal(err)
	}
	verifier, err := nodeagent.NewSignedArtifactVerifier(repository.trust)
	if err != nil {
		t.Fatal(err)
	}
	store := nodeagent.NewMemoryStateStore()
	reconciler := &nodeagent.Reconciler{
		NodeID: nodeID, NodeLabels: map[string]string{"region": "p5"}, Sources: []nodeagent.ArtifactSource{repository}, Verifier: verifier,
		Installer: nodeagent.LocalInstaller{Root: t.TempDir()}, Runtime: pluginRuntime,
		StateStore: nodeagent.ReplicatedStateStore{Primary: store, Replicas: []nodeagent.StateStore{
			nodeagent.NATSStateStore{KV: buckets.Actual, Key: controlplane.ActualKey("acme", "p5-pipeline", nodeID)},
		}},
	}
	ctx, cancel := context.WithCancel(context.Background())
	lease, err := controlplane.StartNodeLease(ctx, buckets.Nodes, nodeID, map[string]string{"region": "p5", "os": runtime.GOOS}, controlplane.NodeLeaseOptions{
		TenantID: "acme", Deployment: "p5-pipeline", AllowUnattested: true,
	})
	if err != nil {
		cancel()
		_ = pluginRuntime.Close()
		_ = router.Close()
		conn.Close()
		t.Fatal(err)
	}
	agent := &nodeagent.Agent{
		Source:     nodeagent.NATSDesiredStateSource{KV: buckets.Assignments, Key: controlplane.AssignmentKey("acme", "p5-pipeline", nodeID), Conn: conn},
		Reconciler: reconciler, Interval: time.Hour, RetryMin: 20 * time.Millisecond, RetryMax: 100 * time.Millisecond, Logf: t.Logf,
	}
	node := &clusterNode{
		id: nodeID, conn: conn, router: router, runtime: pluginRuntime, reconciler: reconciler,
		store: store, lease: lease, cancel: cancel, done: make(chan error, 1),
	}
	go func() { node.done <- agent.Run(ctx) }()
	return node
}

func p5InstalledPluginIDs(values []nodeagent.InstalledPlugin) []string {
	ids := make([]string, 0, len(values))
	for _, value := range values {
		ids = append(ids, value.ID)
	}
	sort.Strings(ids)
	return ids
}
