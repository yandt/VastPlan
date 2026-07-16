// Backend 内核（MVP 骨架）。
//
// 内核只提供最小骨架（系统架构 §1.4）：扩展点注册表 + 协议总线 + 生命周期。
// 不含业务——业务一律下沉为插件。
//
// 本 MVP 跑通最小闭环：声明扩展点 → 拉起插件 → 握手/engines 校验 → 贡献注册
// → 激活 → 调用（含插件回调宿主）→ 摘除。
// 支持两条入口：直接传插件二进制用于协议演示；reconcile 子命令运行 Node Agent 自动装配。
// 控制面模式同时接入 NATS KV 与跨节点 capability 寻址。
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"cdsoft.com.cn/VastPlan/kernels/backend/hostfactory"
	"cdsoft.com.cn/VastPlan/kernels/backend/nodeagent"
	"cdsoft.com.cn/VastPlan/kernels/backend/pluginservice"
	"cdsoft.com.cn/VastPlan/shared/go/addressing"
	contractv1 "cdsoft.com.cn/VastPlan/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/shared/go/controlplane"
)

// KernelName 本内核的规范 ID（ADR-0015）。
const KernelName = hostfactory.KernelName

// version 由构建时注入：-ldflags "-X main.version=$(cat kernels/backend/VERSION)"
// 单一真源是 kernels/backend/VERSION（ADR-0017 §1）；devel 仅用于未经构建脚本的本地跑。
var version = "0.0.0-devel"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(2)
	}
	if os.Args[1] == "reconcile" {
		if err := runReconcile(os.Args[2:]); err != nil {
			log.Fatalf("[node-agent] %v", err)
		}
		return
	}
	runDemo(os.Args[1:])
}

func printUsage() {
	name := filepath.Base(os.Args[0])
	fmt.Fprintf(os.Stderr, "用法:\n  %s <插件可执行文件路径>...\n  %s reconcile -desired <期望态.json> [参数]\n  %s reconcile -nats-url <URL> -deployment <name> -node-id <id> [参数]\n", name, name, name)
}

func runDemo(pluginBins []string) {
	logf := func(format string, args ...any) { log.Printf("[kernel] "+format, args...) }
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	logf("内核 %s@%s 启动", KernelName, version)

	// ── 1. 起宿主（扩展点与内置能力由两条启动路径共享）────────
	host, err := hostfactory.New(version, logf)
	if err != nil {
		log.Fatalf("[kernel] 创建宿主失败: %v", err)
	}
	if err := host.Start(); err != nil {
		log.Fatalf("[kernel] 宿主启动失败: %v", err)
	}
	defer host.Stop()

	// ── 2. 装载插件：握手 → engines 校验 → 贡献注册 → 激活 ──
	// 权限校验器由插件提供（ADR-0001 一切功能皆插件）；没装它则所有调用被
	// fail-closed 拒绝（ADR-0021）——这正是要演示的。
	for _, bin := range pluginBins {
		if _, err := host.Launch(ctx, bin); err != nil {
			log.Fatalf("[kernel] 装载插件 %s 失败: %v", filepath.Base(bin), err)
		}
	}

	// ── 3. 调用插件贡献的能力（契约全程透传）─────────────────
	callCtx := &contractv1.CallContext{
		Principal: &contractv1.Principal{
			UserId: "u-1001", Username: "zhanghui", TenantId: "acme", IsAdmin: true,
		},
		Caller:   &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_AGENT, Id: "agent-42"},
		Scene:    "agent.tool_call", // 三元组之 scene
		TenantId: "acme",
		Trace:    &contractv1.Trace{TraceId: "trace-abc123", SpanId: "span-1"},
	}

	for _, tc := range []struct{ op, payload string }{
		{"greet", `{"name":"VastPlan"}`},
		{"echo", `{"text":"契约与协议跑通了"}`},
		{"whoami", `{}`},         // 插件回调宿主取内核信息
		{"greet", `{"name":""}`}, // 应用层错误
		{"nope", `{}`},           // 未实现操作
	} {
		op := tc.op
		target := &contractv1.CallTarget{
			ExtensionPoint: "tool.package",
			Capability:     "vastplan.hello", // 四处同名：清单 id = 注册名 = capability
			Operation:      &op,
		}
		resp, err := host.Invoke(ctx, target, callCtx, []byte(tc.payload))
		if err != nil {
			logf("调用 %s 传输层失败: %v", op, err) // 传输层错误与应用层错误严格区分
			continue
		}
		if resp.Result.Status == contractv1.CallResult_STATUS_OK {
			logf("调用 %s → OK (%dms) %s", op, resp.Result.Usage.DurationMs, pretty(resp.Payload))
		} else {
			logf("调用 %s → 应用层错误 code=%s retryable=%v msg=%s",
				op, resp.Result.Error.Code, resp.Result.Error.Retryable, resp.Result.Error.Message)
		}
	}

	// ── 4. 未注册能力的解析应失败（fail-closed）──────────────
	if _, err := host.Invoke(ctx, &contractv1.CallTarget{
		ExtensionPoint: "tool.package", Capability: "not.registered",
	}, callCtx, nil); err != nil {
		logf("未注册能力被正确拒绝: %v", err)
	}

	// ── 5. fanout：发布事件，扇出给所有订阅者 ────────────────
	outcomes := host.PublishEvent(ctx, &contractv1.CallEvent{
		Id: "evt-1", Type: "task.completed", Source: "kernel",
		TenantId: "acme", OccurredAtUnixMs: time.Now().UnixMilli(),
		Trace:   callCtx.Trace,
		Payload: []byte(`{"taskId":"t-1"}`),
	})
	for _, o := range outcomes {
		if o.Err != nil {
			logf("事件汇 %s 失败: %v", o.SinkID, o.Err)
		} else {
			logf("事件已投递给 %s", o.SinkID)
		}
	}
	// 未被任何 sink 订阅的类型：不应投递
	if n := len(host.PublishEvent(ctx, &contractv1.CallEvent{
		Id: "evt-2", Type: "unsubscribed.type", Source: "kernel", TenantId: "acme",
	})); n == 0 {
		logf("未订阅的事件类型无人接收（符合预期）")
	}

	// 向审计插件查账：验证事件**真的送达了插件**，而非宿主自说自话
	if resp, err := host.Invoke(ctx, &contractv1.CallTarget{
		ExtensionPoint: "tool.package", Capability: "demo.audit", Operation: ptr("list"),
	}, callCtx, nil); err == nil && resp.Result.Status == contractv1.CallResult_STATUS_OK {
		logf("审计插件账本 → %s", pretty(resp.Payload))
	}

	logf("MVP 闭环完成")
}

func runReconcile(args []string) (runErr error) {
	flags := flag.NewFlagSet("reconcile", flag.ContinueOnError)
	desiredPath := flags.String("desired", "", "本地 DesiredState v1 JSON 文件")
	repositoryRoot := flags.String("repository", ".vastplan/repository", "本地插件制品仓库")
	repositoryURL := flags.String("repository-url", "", "HTTPS 远端签名制品仓库；设置后替代本地仓库")
	repositoryTrust := flags.String("repository-trust", "", "远端制品发布者信任文档")
	repositoryToken := flags.String("repository-token", "", "远端制品读令牌；默认读取 VASTPLAN_ARTIFACT_READ_TOKEN")
	repositoryCA := flags.String("repository-ca", "", "远端制品仓库自定义 CA PEM")
	runtimeRoot := flags.String("runtime-root", ".vastplan/runtime/plugins", "内容寻址安装目录")
	actualPath := flags.String("actual-state", ".vastplan/runtime/actual-state.json", "实际态报告文件")
	lockPath := flags.String("lock", "", "单实例锁文件；默认 <actual-state>.lock")
	nodeID := flags.String("node-id", "local", "当前节点 ID")
	labelsRaw := flags.String("labels", "", "节点标签，逗号分隔 key=value")
	capacityCPU := flags.Int64("capacity-cpu-millis", 0, "节点可分配 CPU，单位 millicores")
	capacityMemory := flags.Int64("capacity-memory-bytes", 0, "节点可分配内存，单位 bytes")
	capacityGPU := flags.Int64("capacity-gpu", 0, "节点可分配 GPU 数量")
	interval := flags.Duration("interval", 5*time.Second, "本地期望态轮询间隔")
	natsURL := flags.String("nats-url", "", "NATS URL；设置后从 JetStream KV watch 期望态")
	natsCA := flags.String("nats-ca", "", "NATS 服务端/客户端证书 CA PEM")
	natsCert := flags.String("nats-cert", "", "NATS mTLS 客户端证书 PEM")
	natsKey := flags.String("nats-key", "", "NATS mTLS 客户端私钥 PEM")
	natsSeed := flags.String("nats-seed", "", "NATS 角色 NKey seed 文件（0600）")
	natsAllowInsecure := flags.Bool("nats-allow-insecure", false, "仅本地开发：允许明文匿名 NATS")
	desiredKey := flags.String("desired-key", controlplane.DesiredKey("", "local-development"), "NATS DesiredState key")
	natsBootstrap := flags.Bool("nats-bootstrap", false, "创建/校准控制面 KV bucket（仅初始化/开发使用）")
	natsReplicas := flags.Int("nats-replicas", 1, "初始化 KV bucket 的 JetStream 副本数；生产建议至少 3")
	assignmentKey := flags.String("assignment-key", "", "节点级 assignment key；设置后从 ASSIGNMENTS_V1 消费，覆盖 -desired-key")
	deploymentName := flags.String("deployment", "", "集群 Deployment v2 名称；自动生成当前节点 assignment key")
	deploymentTenant := flags.String("tenant", "", "集群 Deployment v2 租户；与 -deployment 一起使用")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *deploymentName != "" {
		if *assignmentKey != "" {
			return errors.New("-deployment 与 -assignment-key 不能同时设置")
		}
		*assignmentKey = controlplane.AssignmentKey(*deploymentTenant, *deploymentName, *nodeID)
	}
	if *desiredPath == "" && *natsURL == "" {
		return errors.New("本地模式必须提供 -desired；控制面模式须提供 -nats-url")
	}
	if *lockPath == "" {
		*lockPath = *actualPath + ".lock"
	}
	processLock, err := nodeagent.AcquireProcessLock(*lockPath)
	if err != nil {
		return err
	}
	defer func() { runErr = errors.Join(runErr, processLock.Close()) }()
	labels, err := parseLabels(*labelsRaw)
	if err != nil {
		return err
	}
	var repository nodeagent.ArtifactRepository
	if *repositoryURL == "" {
		localRepository, createErr := pluginservice.NewRepository(*repositoryRoot)
		if createErr != nil {
			return createErr
		}
		repository = localRepository
	} else {
		if *repositoryTrust == "" {
			return errors.New("远端制品仓库必须配置 -repository-trust")
		}
		trust, loadErr := pluginservice.LoadTrustStore(*repositoryTrust)
		if loadErr != nil {
			return loadErr
		}
		if *repositoryToken == "" {
			*repositoryToken = os.Getenv("VASTPLAN_ARTIFACT_READ_TOKEN")
		}
		if *repositoryToken == "" {
			return errors.New("远端制品仓库必须配置读令牌")
		}
		httpClient, clientErr := artifactHTTPClient(*repositoryCA)
		if clientErr != nil {
			return clientErr
		}
		repository = &pluginservice.RemoteRepository{
			BaseURL: *repositoryURL, Token: *repositoryToken, Trust: trust, Client: httpClient,
		}
	}
	logf := func(format string, values ...any) { log.Printf("[node-agent] "+format, values...) }
	var source nodeagent.DesiredStateSource = nodeagent.FileSource{Path: *desiredPath}
	var stateStore nodeagent.StateStore = nodeagent.FileStateStore{Path: *actualPath}
	var meshRouter *addressing.Router
	var natsBuckets controlplane.Buckets
	var closeNATS func()
	if *natsURL != "" {
		nc, err := controlplane.ConnectWithConfig(controlplane.ConnectionConfig{
			URL: *natsURL, ClientName: "vastplan-node-" + *nodeID,
			CAFile: *natsCA, CertFile: *natsCert, KeyFile: *natsKey, SeedFile: *natsSeed,
			Insecure: *natsAllowInsecure, Logf: logf,
		})
		if err != nil {
			return err
		}
		closeNATS = nc.Close
		defer closeNATS()
		js, err := jetstream.New(nc)
		if err != nil {
			return fmt.Errorf("创建 JetStream 客户端: %w", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		var buckets controlplane.Buckets
		if *natsBootstrap {
			buckets, err = controlplane.EnsureBuckets(ctx, js, *natsReplicas, jetstream.FileStorage)
		} else {
			buckets, err = controlplane.OpenBuckets(ctx, js)
		}
		if err != nil {
			return err
		}
		natsBuckets = buckets
		if *assignmentKey != "" {
			source = nodeagent.NATSDesiredStateSource{KV: buckets.Assignments, Key: *assignmentKey, Conn: nc}
		} else {
			source = nodeagent.NATSDesiredStateSource{KV: buckets.Desired, Key: *desiredKey, Conn: nc}
		}
		stateStore = nodeagent.ReplicatedStateStore{
			Primary: nodeagent.FileStateStore{Path: *actualPath},
			Replicas: []nodeagent.StateStore{
				nodeagent.NATSStateStore{KV: buckets.Actual, Key: controlplane.ActualKey(*nodeID)},
			},
		}
		meshRouter, err = addressing.NewRouter(nc, buckets.Capabilities, *nodeID, logf)
		if err != nil {
			return fmt.Errorf("创建 capability router: %w", err)
		}
		defer func() { runErr = errors.Join(runErr, meshRouter.Close()) }()
	}
	runtime := nodeagent.NewProtocolRuntime(version, logf)
	defer func() { runErr = errors.Join(runErr, runtime.Close()) }()
	if meshRouter != nil {
		if err := runtime.AttachRouter(meshRouter); err != nil {
			return err
		}
	}
	reconciler := &nodeagent.Reconciler{
		NodeID: *nodeID, NodeLabels: labels, Repository: repository,
		Installer: nodeagent.LocalInstaller{Root: *runtimeRoot}, Runtime: runtime,
		StateStore: stateStore,
	}
	agent := &nodeagent.Agent{
		Source: source, Reconciler: reconciler,
		Interval: *interval, Logf: logf,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var nodeLease *controlplane.NodeLease
	var leaseLost <-chan error
	var leaseFailure chan error
	if *natsURL != "" {
		if *assignmentKey != "" {
			assignmentNodeID, parseErr := controlplane.AssignmentKeyNodeID(*assignmentKey)
			if parseErr != nil || assignmentNodeID != *nodeID {
				return fmt.Errorf("assignment key 不属于当前节点 %s", *nodeID)
			}
			// 先删除同 node id 上一进程遗留的快照，再发布新租约。控制器观察到租约后会
			// 重新下发当前计划；若控制器不可用，本节点保持空载而不是执行陈旧 assignment。
			if deleteErr := natsBuckets.Assignments.Delete(ctx, *assignmentKey); deleteErr != nil && !errors.Is(deleteErr, jetstream.ErrKeyNotFound) {
				return fmt.Errorf("作废旧 assignment: %w", deleteErr)
			}
		}
		nodeLease, err = controlplane.StartNodeLease(ctx, natsBuckets.Nodes, *nodeID, labels, controlplane.NodeLeaseOptions{
			Logf: logf, Capacity: controlplane.ResourceCapacity{
				CPUMillis: *capacityCPU, MemoryBytes: *capacityMemory, GPU: *capacityGPU,
			},
		})
		if err != nil {
			return err
		}
		leaseLost = nodeLease.Lost()
		leaseFailure = make(chan error, 1)
		defer func() {
			closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = nodeLease.Close(closeCtx)
		}()
		go func() {
			select {
			case leaseErr := <-leaseLost:
				leaseFailure <- leaseErr
				logf("节点失去控制面租约，将自我隔离并停止 unit: %v", leaseErr)
				stop()
			case <-ctx.Done():
			}
		}()
	}
	if *natsURL != "" {
		activeKey := *desiredKey
		if *assignmentKey != "" {
			activeKey = *assignmentKey
		}
		logf("节点 %s 启动，NATS=%s desired-key=%s", *nodeID, *natsURL, activeKey)
	} else {
		logf("节点 %s 启动，期望态=%s", *nodeID, *desiredPath)
	}
	err = agent.Run(ctx)
	if errors.Is(err, context.Canceled) {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if nodeLease != nil {
			_ = nodeLease.Close(shutdownCtx)
		}
		shutdownErr := reconciler.Shutdown(shutdownCtx)
		select {
		case leaseErr := <-leaseFailure:
			return errors.Join(leaseErr, shutdownErr)
		default:
			return shutdownErr
		}
	}
	return err
}

func artifactHTTPClient(caFile string) (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if caFile != "" {
		raw, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("读取远端制品仓库 CA: %w", err)
		}
		roots, err := x509.SystemCertPool()
		if err != nil || roots == nil {
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM(raw) {
			return nil, errors.New("远端制品仓库 CA PEM 不包含有效证书")
		}
		transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	}
	return &http.Client{Transport: transport, Timeout: 5 * time.Minute}, nil
}

func parseLabels(raw string) (map[string]string, error) {
	labels := map[string]string{}
	if strings.TrimSpace(raw) == "" {
		return labels, nil
	}
	for _, item := range strings.Split(raw, ",") {
		key, value, ok := strings.Cut(item, "=")
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if !ok || key == "" || value == "" {
			return nil, fmt.Errorf("非法节点标签 %q，须为 key=value", item)
		}
		if _, exists := labels[key]; exists {
			return nil, fmt.Errorf("节点标签重复: %s", key)
		}
		labels[key] = value
	}
	return labels, nil
}

func ptr(s string) *string { return &s }

func pretty(b []byte) string {
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return string(b)
	}
	out, _ := json.Marshal(v)
	return string(out)
}
