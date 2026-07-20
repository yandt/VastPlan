package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/nodeagent"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/pluginservice"
	"cdsoft.com.cn/VastPlan/core/shared/go/addressing"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifacttrust"
	"cdsoft.com.cn/VastPlan/core/shared/go/controlplane"
)

type reconcileOptions struct {
	desiredPath, repositoryRoot, repositoryURL, repositoryTrust, repositoryToken, repositoryCA string
	bootstrapRepository                                                                        string
	runtimeRoot, actualPath, lockPath, nodeID, labelsRaw                                       string
	credentialRoot                                                                             string
	backendPlatformCatalog                                                                     string
	firstPartyPublishers                                                                       string
	thirdPartyPluginPolicy, publisherPluginPolicies                                            string
	defaultPluginContextAccess, publisherPluginContextAccess                                   string
	pluginPlacementDefault, publisherPluginPlacements, pluginPlacements                        string
	runtimeHostingDefault, publisherRuntimeHosting, pluginRuntimeHosting                       string
	capacityCPU, capacityMemory, capacityGPU                                                   int64
	interval                                                                                   time.Duration
	natsURL, natsCA, natsCert, natsKey, natsSeed, transportSeed, transportTrust                string
	natsAllowInsecure, natsBootstrap, allowDevelopmentPlugins                                  bool
	requireThirdPartyIsolation                                                                 bool
	executionPolicy                                                                            nodeagent.ExecutionPolicy
	contextPolicy                                                                              nodeagent.ContextPolicy
	placementPolicy                                                                            nodeagent.PlacementPolicy
	hostingPolicy                                                                              nodeagent.RuntimeHostingPolicy
	desiredKey, assignmentKey, deploymentName, deploymentTenant                                string
	natsReplicas                                                                               int
}

func parseReconcileOptions(args []string) (reconcileOptions, error) {
	var options reconcileOptions
	flags := flag.NewFlagSet("reconcile", flag.ContinueOnError)
	flags.StringVar(&options.desiredPath, "desired", "", "本地 DesiredState v1 JSON 文件")
	flags.StringVar(&options.repositoryRoot, "repository", ".vastplan/repository", "本地插件制品仓库")
	flags.StringVar(&options.repositoryURL, "repository-url", "", "HTTPS 远端签名制品仓库；设置后替代本地仓库")
	flags.StringVar(&options.repositoryTrust, "repository-trust", "", "远端制品发布者信任文档")
	flags.StringVar(&options.repositoryToken, "repository-token", "", "远端制品读令牌；默认读取 VASTPLAN_ARTIFACT_READ_TOKEN")
	flags.StringVar(&options.repositoryCA, "repository-ca", "", "远端制品仓库自定义 CA PEM")
	flags.StringVar(&options.bootstrapRepository, "bootstrap-repository", "", "预置签名种子仓库；精确命中时优先于远端源")
	flags.StringVar(&options.runtimeRoot, "runtime-root", ".vastplan/runtime/plugins", "内容寻址安装目录")
	flags.StringVar(&options.actualPath, "actual-state", ".vastplan/runtime/actual-state.json", "实际态报告文件")
	flags.StringVar(&options.lockPath, "lock", "", "单实例锁文件；默认 <actual-state>.lock")
	flags.StringVar(&options.nodeID, "node-id", "local", "当前节点 ID")
	flags.StringVar(&options.labelsRaw, "labels", "", "节点标签，逗号分隔 key=value")
	flags.StringVar(&options.credentialRoot, "credential-root", "", "可信凭证挂载根目录：<root>/<tenant>/<credential-name>；留空不启用节点引导 Broker")
	flags.StringVar(&options.backendPlatformCatalog, "backend-platform-catalog", "", "平台签发的 Backend Platform Catalog；配置后向 deployment-manager 开放在线编排")
	flags.StringVar(&options.thirdPartyPluginPolicy, "third-party-plugin-policy", string(nodeagent.PublisherPolicyRequireIsolation), "未单独配置发布者时的策略: require-isolation, allow-trusted, deny")
	flags.StringVar(&options.publisherPluginPolicies, "publisher-plugin-policies", "", "发布者级策略，逗号分隔 publisher=policy；优先于全局策略")
	flags.StringVar(&options.defaultPluginContextAccess, "default-plugin-context-access", "", "未知发布者的 CallContext 字段上限，逗号分隔；空值使用安全默认")
	flags.StringVar(&options.publisherPluginContextAccess, "publisher-plugin-context-access", "", "发布者级 CallContext 上限，分号分隔 publisher=field,field；* 表示全部已知字段")
	flags.StringVar(&options.firstPartyPublishers, "first-party-publishers", "vastplan", "兼容参数：隐式配置 allow-trusted 的发布者，逗号分隔；显式发布者策略优先")
	flags.StringVar(&options.pluginPlacementDefault, "plugin-placement-default", string(nodeagent.PlacementProcessOnly), "插件默认放置: process-only, prefer-dynamic-go, require-dynamic-go")
	flags.StringVar(&options.publisherPluginPlacements, "publisher-plugin-placements", "", "发布者级放置策略，逗号分隔 publisher=mode")
	flags.StringVar(&options.pluginPlacements, "plugin-placements", "", "插件级放置策略，逗号分隔 pluginID=mode；优先级最高")
	flags.StringVar(&options.runtimeHostingDefault, "runtime-hosting-default", string(nodeagent.RuntimeHostingShared), "托管语言插件默认 Host 模式: shared, dedicated")
	flags.StringVar(&options.publisherRuntimeHosting, "publisher-runtime-hosting", "", "发布者级 Runtime Host 模式，逗号分隔 publisher=mode")
	flags.StringVar(&options.pluginRuntimeHosting, "plugin-runtime-hosting", "", "插件级 Runtime Host 模式，逗号分隔 pluginID=mode；优先级最高")
	flags.BoolVar(&options.requireThirdPartyIsolation, "require-third-party-isolation", true, "已弃用兼容参数；请使用 -third-party-plugin-policy")
	flags.Int64Var(&options.capacityCPU, "capacity-cpu-millis", 0, "节点可分配 CPU，单位 millicores")
	flags.Int64Var(&options.capacityMemory, "capacity-memory-bytes", 0, "节点可分配内存，单位 bytes")
	flags.Int64Var(&options.capacityGPU, "capacity-gpu", 0, "节点可分配 GPU 数量")
	flags.DurationVar(&options.interval, "interval", 5*time.Second, "本地期望态轮询间隔")
	flags.StringVar(&options.natsURL, "nats-url", "", "NATS URL；设置后从 JetStream KV watch 期望态")
	flags.StringVar(&options.natsCA, "nats-ca", "", "NATS 服务端/客户端证书 CA PEM")
	flags.StringVar(&options.natsCert, "nats-cert", "", "NATS mTLS 客户端证书 PEM")
	flags.StringVar(&options.natsKey, "nats-key", "", "NATS mTLS 客户端私钥 PEM")
	flags.StringVar(&options.natsSeed, "nats-seed", "", "NATS 角色 NKey seed 文件（0600）")
	flags.StringVar(&options.transportSeed, "transport-seed", "", "addressing 传输身份 NKey seed 文件（0600）")
	flags.StringVar(&options.transportTrust, "transport-trust", "", "addressing 传输身份信任文档 JSON")
	flags.BoolVar(&options.natsAllowInsecure, "nats-allow-insecure", false, "仅本地开发：允许明文匿名 NATS")
	flags.BoolVar(&options.allowDevelopmentPlugins, "allow-development-plugins", false, "仅本地开发：允许在线组合使用 example 或历史未分类首方插件")
	flags.StringVar(&options.desiredKey, "desired-key", controlplane.DesiredKey("", "local-development"), "NATS DesiredState key")
	flags.BoolVar(&options.natsBootstrap, "nats-bootstrap", false, "创建/校准控制面 KV bucket（仅初始化/开发使用）")
	flags.IntVar(&options.natsReplicas, "nats-replicas", 1, "初始化 KV bucket 的 JetStream 副本数；生产建议至少 3")
	flags.StringVar(&options.assignmentKey, "assignment-key", "", "节点级 assignment key；设置后从 ASSIGNMENTS_V1 消费，覆盖 -desired-key")
	flags.StringVar(&options.deploymentName, "deployment", "", "集群 Deployment v2 名称；自动生成当前节点 assignment key")
	flags.StringVar(&options.deploymentTenant, "tenant", "", "集群 Deployment v2 租户；与 -deployment 一起使用")
	if err := flags.Parse(args); err != nil {
		return reconcileOptions{}, err
	}
	visited := map[string]bool{}
	flags.Visit(func(item *flag.Flag) { visited[item.Name] = true })
	if visited["require-third-party-isolation"] {
		if visited["third-party-plugin-policy"] {
			return reconcileOptions{}, errors.New("-require-third-party-isolation 与 -third-party-plugin-policy 不能同时设置")
		}
		if options.requireThirdPartyIsolation {
			options.thirdPartyPluginPolicy = string(nodeagent.PublisherPolicyRequireIsolation)
		} else {
			options.thirdPartyPluginPolicy = string(nodeagent.PublisherPolicyAllowTrusted)
		}
	}
	var err error
	options.executionPolicy, err = nodeagent.ParseExecutionPolicy(
		options.thirdPartyPluginPolicy,
		options.publisherPluginPolicies,
		strings.Split(options.firstPartyPublishers, ","),
	)
	if err != nil {
		return reconcileOptions{}, err
	}
	options.contextPolicy, err = nodeagent.ParseContextPolicy(options.defaultPluginContextAccess, options.publisherPluginContextAccess)
	if err != nil {
		return reconcileOptions{}, err
	}
	options.placementPolicy, err = nodeagent.ParsePlacementPolicy(
		options.pluginPlacementDefault, options.publisherPluginPlacements, options.pluginPlacements,
	)
	if err != nil {
		return reconcileOptions{}, err
	}
	options.hostingPolicy, err = nodeagent.ParseRuntimeHostingPolicy(
		options.runtimeHostingDefault, options.publisherRuntimeHosting, options.pluginRuntimeHosting,
	)
	if err != nil {
		return reconcileOptions{}, err
	}
	if options.deploymentName != "" {
		if options.assignmentKey != "" {
			return reconcileOptions{}, errors.New("-deployment 与 -assignment-key 不能同时设置")
		}
		options.assignmentKey = controlplane.AssignmentKey(options.deploymentTenant, options.deploymentName, options.nodeID)
	}
	if options.desiredPath == "" && options.natsURL == "" {
		return reconcileOptions{}, errors.New("本地模式必须提供 -desired；控制面模式须提供 -nats-url")
	}
	if options.backendPlatformCatalog != "" && options.natsURL == "" {
		return reconcileOptions{}, errors.New("在线部署发布必须同时配置 -backend-platform-catalog 与 -nats-url")
	}
	if options.lockPath == "" {
		options.lockPath = options.actualPath + ".lock"
	}
	if options.credentialRoot != "" && (!filepath.IsAbs(options.credentialRoot) || filepath.Clean(options.credentialRoot) != options.credentialRoot) {
		return reconcileOptions{}, errors.New("-credential-root 必须是规范绝对路径")
	}
	if options.natsURL != "" && options.assignmentKey != "" {
		assignmentNodeID, err := controlplane.AssignmentKeyNodeID(options.assignmentKey)
		if err != nil || assignmentNodeID != options.nodeID {
			return reconcileOptions{}, fmt.Errorf("assignment key 不属于当前节点 %s", options.nodeID)
		}
	}
	return options, nil
}

type artifactResolution struct {
	sources  []nodeagent.ArtifactSource
	verifier nodeagent.ArtifactVerifier
}

// Read implements the resolver's synchronous immutable ArtifactReader on top
// of the same ordered sources and mandatory verifier used by Node Agent. A
// source that returns untrusted bytes is a hard failure and cannot be hidden by
// trying the next source.
func (r artifactResolution) Read(ref pluginv1.ArtifactRef) (pluginv1.Artifact, []byte, error) {
	var notFound error
	for _, source := range r.sources {
		if source == nil {
			return pluginv1.Artifact{}, nil, errors.New("制品源不能为空")
		}
		envelope, err := source.Fetch(context.Background(), ref)
		if errors.Is(err, artifacttrust.ErrNotFound) {
			notFound = errors.Join(notFound, err)
			continue
		}
		if err != nil {
			return pluginv1.Artifact{}, nil, fmt.Errorf("制品源 %T 失败: %w", source, err)
		}
		verified, err := r.verifier.Verify(ref, envelope)
		if err != nil {
			return pluginv1.Artifact{}, nil, fmt.Errorf("制品源 %T 返回不可信内容: %w", source, err)
		}
		return verified.Artifact(), verified.PackageBytes(), nil
	}
	if notFound != nil {
		return pluginv1.Artifact{}, nil, fmt.Errorf("所有制品源均无此制品: %w", notFound)
	}
	return pluginv1.Artifact{}, nil, errors.New("没有可用制品源")
}

func buildArtifactResolution(options reconcileOptions) (artifactResolution, error) {
	if options.repositoryURL == "" && options.bootstrapRepository == "" {
		local, err := pluginservice.NewRepository(options.repositoryRoot)
		if err != nil {
			return artifactResolution{}, err
		}
		return artifactResolution{
			sources: []nodeagent.ArtifactSource{local}, verifier: nodeagent.NewLocalDevelopmentArtifactVerifier(),
		}, nil
	}
	if options.repositoryTrust == "" {
		return artifactResolution{}, errors.New("远端或种子制品源必须配置 -repository-trust")
	}
	trust, err := pluginservice.LoadTrustStore(options.repositoryTrust)
	if err != nil {
		return artifactResolution{}, err
	}
	verifier, err := nodeagent.NewSignedArtifactVerifier(trust)
	if err != nil {
		return artifactResolution{}, err
	}
	resolution := artifactResolution{verifier: verifier}
	if options.bootstrapRepository != "" {
		local, err := pluginservice.NewRepository(options.bootstrapRepository)
		if err != nil {
			return artifactResolution{}, err
		}
		resolution.sources = append(resolution.sources, &pluginservice.SignedRepository{Local: local, Trust: trust})
	}
	if options.repositoryURL != "" {
		token := options.repositoryToken
		if token == "" {
			token = os.Getenv("VASTPLAN_ARTIFACT_READ_TOKEN")
		}
		if token == "" {
			return artifactResolution{}, errors.New("远端制品仓库必须配置读令牌")
		}
		httpClient, err := artifactHTTPClient(options.repositoryCA)
		if err != nil {
			return artifactResolution{}, err
		}
		resolution.sources = append(resolution.sources, &pluginservice.RemoteRepository{
			BaseURL: options.repositoryURL, Token: token, Trust: trust, Client: httpClient,
		})
	}
	return resolution, nil
}

type nodeControlPlane struct {
	source     nodeagent.DesiredStateSource
	stateStore nodeagent.StateStore
	router     *addressing.Router
	transport  *addressing.TransportSecurity
	buckets    controlplane.Buckets
	closeNATS  func()
}

func newNodeControlPlane(options reconcileOptions, logf func(string, ...any)) (*nodeControlPlane, error) {
	plane := &nodeControlPlane{
		source:     nodeagent.FileSource{Path: options.desiredPath},
		stateStore: nodeagent.FileStateStore{Path: options.actualPath},
	}
	if options.natsURL == "" {
		return plane, nil
	}
	if (options.transportSeed == "") != (options.transportTrust == "") {
		return nil, errors.New("addressing 传输身份必须同时配置 -transport-seed 和 -transport-trust")
	}
	if !options.natsAllowInsecure && options.transportSeed == "" {
		return nil, errors.New("生产控制面必须配置 addressing 传输身份；本地开发请显式使用 -nats-allow-insecure")
	}
	var err error
	if options.transportSeed != "" {
		plane.transport, err = addressing.LoadTransportSecurity(options.transportSeed, options.transportTrust)
		if err != nil {
			return nil, err
		}
	}
	nc, err := controlplane.ConnectWithConfig(controlplane.ConnectionConfig{
		URL: options.natsURL, ClientName: "vastplan-node-" + options.nodeID,
		CAFile: options.natsCA, CertFile: options.natsCert, KeyFile: options.natsKey, SeedFile: options.natsSeed,
		Insecure: options.natsAllowInsecure, Logf: logf,
	})
	if err != nil {
		if plane.transport != nil {
			plane.transport.Close()
		}
		return nil, err
	}
	plane.closeNATS = nc.Close
	js, err := jetstream.New(nc)
	if err != nil {
		_ = plane.Close() // 初始化尚未交给调用方，优先返回创建失败。
		return nil, fmt.Errorf("创建 JetStream 客户端: %w", err)
	}
	openCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if options.natsBootstrap {
		plane.buckets, err = controlplane.EnsureBuckets(openCtx, js, options.natsReplicas, jetstream.FileStorage)
	} else {
		plane.buckets, err = controlplane.OpenBuckets(openCtx, js)
	}
	if err != nil {
		_ = plane.Close() // 初始化尚未交给调用方，优先返回 bucket 失败。
		return nil, err
	}
	if options.assignmentKey != "" {
		plane.source = nodeagent.NATSDesiredStateSource{KV: plane.buckets.Assignments, Key: options.assignmentKey, Conn: nc}
	} else {
		plane.source = nodeagent.NATSDesiredStateSource{KV: plane.buckets.Desired, Key: options.desiredKey, Conn: nc}
	}
	tenant, deployment := controlPlaneScope(options.deploymentTenant, options.deploymentName)
	plane.stateStore = nodeagent.ReplicatedStateStore{
		Primary: nodeagent.FileStateStore{Path: options.actualPath},
		Replicas: []nodeagent.StateStore{
			nodeagent.NATSStateStore{KV: plane.buckets.Actual, Key: controlplane.ActualKey(tenant, deployment, options.nodeID)},
		},
	}
	if plane.transport != nil {
		plane.router, err = addressing.NewSecureRouter(nc, plane.buckets.Capabilities, options.nodeID, logf, plane.transport)
	} else {
		plane.router, err = addressing.NewRouter(nc, plane.buckets.Capabilities, options.nodeID, logf)
	}
	if err != nil {
		_ = plane.Close() // 初始化尚未交给调用方，优先返回 router 失败。
		return nil, fmt.Errorf("创建 capability router: %w", err)
	}
	return plane, nil
}

func (p *nodeControlPlane) Close() error {
	var err error
	if p != nil && p.router != nil {
		err = p.router.Close()
	}
	if p != nil && p.closeNATS != nil {
		p.closeNATS()
	}
	if p != nil && p.transport != nil {
		p.transport.Close()
	}
	return err
}

type nodeLeaseGuard struct {
	lease   *controlplane.NodeLease
	failure chan error
}

func startNodeLeaseGuard(ctx context.Context, stop context.CancelFunc, options reconcileOptions, labels map[string]string, buckets controlplane.Buckets, transport *addressing.TransportSecurity, logf func(string, ...any)) (*nodeLeaseGuard, error) {
	if options.natsURL == "" {
		return nil, nil
	}
	if options.assignmentKey != "" {
		if err := buckets.Assignments.Delete(ctx, options.assignmentKey); err != nil && !errors.Is(err, jetstream.ErrKeyNotFound) {
			return nil, fmt.Errorf("作废旧 assignment: %w", err)
		}
	}
	tenant, deployment := controlPlaneScope(options.deploymentTenant, options.deploymentName)
	leaseOptions := controlplane.NodeLeaseOptions{
		Logf: logf, TenantID: tenant, Deployment: deployment, AllowUnattested: options.natsAllowInsecure,
		Capacity: controlplane.ResourceCapacity{
			CPUMillis: options.capacityCPU, MemoryBytes: options.capacityMemory, GPU: options.capacityGPU,
		},
	}
	if transport != nil {
		leaseOptions.Attest = transport.AttestNodeLease
	}
	lease, err := controlplane.StartNodeLease(ctx, buckets.Nodes, options.nodeID, labels, leaseOptions)
	if err != nil {
		return nil, err
	}
	guard := &nodeLeaseGuard{lease: lease, failure: make(chan error, 1)}
	go func() {
		select {
		case leaseErr := <-lease.Lost():
			guard.failure <- leaseErr
			logf("节点失去控制面租约，将自我隔离并停止 unit: %v", leaseErr)
			stop()
		case <-ctx.Done():
		}
	}()
	return guard, nil
}

func controlPlaneScope(tenant, deployment string) (string, string) {
	if tenant == "" {
		tenant = "_global"
	}
	if deployment == "" {
		deployment = "legacy"
	}
	return tenant, deployment
}

func (g *nodeLeaseGuard) close(ctx context.Context) error {
	if g == nil || g.lease == nil {
		return nil
	}
	return g.lease.Close(ctx)
}

func (g *nodeLeaseGuard) closeEventually() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = g.close(ctx)
}

func finishCanceledAgent(guard *nodeLeaseGuard, reconciler *nodeagent.Reconciler) error {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	leaseErr := guard.close(shutdownCtx)
	shutdownErr := reconciler.Shutdown(shutdownCtx)
	if guard != nil {
		select {
		case lostErr := <-guard.failure:
			leaseErr = errors.Join(leaseErr, lostErr)
		default:
		}
	}
	return errors.Join(leaseErr, shutdownErr)
}

func logNodeStartup(options reconcileOptions, logf func(string, ...any)) {
	logf("插件运行策略 global=%s publisher-overrides=%s trusted-compat=%s",
		options.thirdPartyPluginPolicy, options.publisherPluginPolicies, options.firstPartyPublishers)
	logf("Runtime Host 策略 default=%s publisher-overrides=%s plugin-overrides=%s",
		options.runtimeHostingDefault, options.publisherRuntimeHosting, options.pluginRuntimeHosting)
	if options.natsURL == "" {
		logf("节点 %s 启动，期望态=%s", options.nodeID, options.desiredPath)
		return
	}
	activeKey := options.desiredKey
	if options.assignmentKey != "" {
		activeKey = options.assignmentKey
	}
	logf("节点 %s 启动，NATS=%s desired-key=%s", options.nodeID, options.natsURL, activeKey)
}
