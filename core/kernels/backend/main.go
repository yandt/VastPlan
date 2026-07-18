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
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	artifactservercommand "cdsoft.com.cn/VastPlan/core/kernels/backend/commands/artifactserver"
	controlplanecommand "cdsoft.com.cn/VastPlan/core/kernels/backend/commands/controlplane"
	nodebootstrapcommand "cdsoft.com.cn/VastPlan/core/kernels/backend/commands/nodebootstrap"
	portaledgecommand "cdsoft.com.cn/VastPlan/core/kernels/backend/commands/portaledge"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/credentialbroker"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/hostfactory"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/kernelops"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/nodeagent"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/nodebootstrapbroker"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/nodebootstrapobserver"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
)

// KernelName 本内核的规范 ID（ADR-0015）。
const KernelName = hostfactory.KernelName

// version 由构建时注入：-ldflags "-X main.version=$(cat core/kernels/backend/VERSION)"
// 单一真源是 core/kernels/backend/VERSION（ADR-0017 §1）；devel 仅用于未经构建脚本的本地跑。
var version = "0.0.0-devel"

// dynamicGoHostFingerprint 由正式构建同时注入 Backend 与首方 .so；空值安全禁用动态加载。
var dynamicGoHostFingerprint string

func init() {
	// JSONHandler 是 Backend 进程的统一结构化出口；slog.SetDefault 同时接管
	// 标准 log 包，保证仍使用 log.Fatal 的启动失败也输出结构化记录。
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))
}

func componentLogf(component string) func(string, ...any) {
	return func(format string, values ...any) { slog.Info(fmt.Sprintf(format, values...), "component", component) }
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "version":
		if err := kernelops.PrintVersion(os.Stdout, version, os.Args[2:]); err != nil {
			log.Fatalf("[version] %v", err)
		}
		return
	case "validate":
		if err := kernelops.RunValidate(os.Stdout, os.Args[2:]); err != nil {
			log.Fatalf("[validate] %v", err)
		}
		return
	case "support-bundle":
		if err := kernelops.RunSupportBundle(os.Stdout, version, os.Args[2:]); err != nil {
			log.Fatalf("[support-bundle] %v", err)
		}
		return
	case "reconcile":
		if err := runReconcile(os.Args[2:]); err != nil {
			log.Fatalf("[node-agent] %v", err)
		}
		return
	case "controlplane":
		runProductionCommand("controlplane", func(ctx context.Context) error {
			return controlplanecommand.Run(ctx, os.Args[2:], os.Stdout, os.Stderr)
		})
		return
	case "artifact-server":
		runProductionCommand("artifact-server", func(ctx context.Context) error {
			return artifactservercommand.Run(ctx, os.Args[2:], os.Stderr)
		})
		return
	case "portal-edge":
		runProductionCommand("portal-edge", func(ctx context.Context) error {
			return portaledgecommand.Run(ctx, os.Args[2:], version, componentLogf("portal-edge"))
		})
		return
	case "node-bootstrap":
		runProductionCommand("node-bootstrap", func(ctx context.Context) error {
			return nodebootstrapcommand.Run(ctx, os.Args[2:], os.Stdout, os.Stderr)
		})
		return
	}
	runDemo(os.Args[1:])
}

func printUsage() {
	name := filepath.Base(os.Args[0])
	fmt.Fprintf(os.Stderr, "用法:\n  %s version [--json]\n  %s validate -kind <desired-v1|platform-profile-v1|application-composition-v1|portal-platform-profile-v1|portal-platform-catalog-v1|portal-application-composition-v1|deployment-v2|actual-state> -file <配置.json>\n  %s support-bundle -actual-state <实际态.json> -output <支持包.tar.gz> [参数]\n  %s <插件可执行文件路径>...\n  %s reconcile -desired <期望态.json> [参数]\n  %s reconcile -nats-url <URL> -deployment <name> -node-id <id> [参数]\n  %s controlplane [参数]\n  %s artifact-server [参数]\n  %s portal-edge [参数]\n  %s node-bootstrap [参数]\n", name, name, name, name, name, name, name, name, name, name)
}

func runProductionCommand(component string, run func(context.Context) error) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil && !errors.Is(err, flag.ErrHelp) && !errors.Is(err, context.Canceled) {
		log.Fatalf("[%s] %v", component, err)
	}
}

func runDemo(pluginBins []string) {
	logf := componentLogf("kernel")
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
	options, err := parseReconcileOptions(args)
	if err != nil {
		return err
	}
	processLock, err := nodeagent.AcquireProcessLock(options.lockPath)
	if err != nil {
		return err
	}
	defer func() { runErr = errors.Join(runErr, processLock.Close()) }()
	labels, err := parseLabels(options.labelsRaw)
	if err != nil {
		return err
	}
	artifacts, err := buildArtifactResolution(options)
	if err != nil {
		return err
	}
	logf := componentLogf("node-agent")
	plane, err := newNodeControlPlane(options, logf)
	if err != nil {
		return err
	}
	defer func() { runErr = errors.Join(runErr, plane.Close()) }()
	runtime := nodeagent.NewProtocolRuntime(version, logf)
	runtime.ExecutionPolicy = options.executionPolicy
	runtime.ContextPolicy = options.contextPolicy
	runtime.PlacementPolicy = options.placementPolicy
	runtime.DynamicGoLoader = nodeagent.NewDynamicGoLoader(dynamicGoHostFingerprint)
	runtime.Identity = options.nodeID
	runtime.LeaderKV = plane.buckets.Controllers
	if plane.transport != nil && plane.buckets.Nodes != nil {
		readiness, err := nodebootstrapobserver.New(plane.buckets.Nodes, plane.transport)
		if err != nil {
			return err
		}
		runtime.Dependencies.NodeReadiness = readiness
	}
	if options.credentialRoot != "" {
		credentials, err := credentialbroker.NewDirectory(options.credentialRoot)
		if err != nil {
			return err
		}
		bootstrapBroker, err := nodebootstrapbroker.NewSSH(credentials, 30*time.Second)
		if err != nil {
			return err
		}
		runtime.Dependencies.Credentials = credentials
		runtime.Dependencies.NodeBootstrap = bootstrapBroker
	}
	defer func() { runErr = errors.Join(runErr, runtime.Close()) }()
	if plane.router != nil {
		if err := runtime.AttachRouter(plane.router); err != nil {
			return err
		}
	}
	reconciler := &nodeagent.Reconciler{
		NodeID: options.nodeID, NodeLabels: labels, Sources: artifacts.sources, Verifier: artifacts.verifier,
		Installer: nodeagent.LocalInstaller{Root: options.runtimeRoot}, Runtime: runtime,
		StateStore: plane.stateStore,
	}
	agent := &nodeagent.Agent{
		Source: plane.source, Reconciler: reconciler,
		Interval: options.interval, Logf: logf,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	leaseGuard, err := startNodeLeaseGuard(ctx, stop, options, labels, plane.buckets, plane.transport, logf)
	if err != nil {
		return err
	}
	defer leaseGuard.closeEventually()
	logNodeStartup(options, logf)
	err = agent.Run(ctx)
	if errors.Is(err, context.Canceled) {
		return finishCanceledAgent(leaseGuard, reconciler)
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
