package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	artifactrepositoryv1 "cdsoft.com.cn/VastPlan/contracts/schemas/artifactrepository/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/nodeagent"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/pluginservice"
	"cdsoft.com.cn/VastPlan/core/shared/go/addressing"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifactrepository"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifactrepository/localtest"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifacttrust"
	"cdsoft.com.cn/VastPlan/core/shared/go/bootstrapinventory"
	"cdsoft.com.cn/VastPlan/core/shared/go/controlplane"
)

type artifactResolution struct {
	sources   []nodeagent.ArtifactSource
	verifier  nodeagent.ArtifactVerifier
	bootstrap *pluginservice.SignedRepository
}

func (r artifactResolution) VerifyBootstrapInventory(ctx context.Context, inventory bootstrapinventory.Inventory) error {
	if r.bootstrap == nil {
		return errors.New("Bootstrap Inventory 已配置但 Seed 仓库源不存在")
	}
	for _, item := range inventory.Seed {
		envelope, err := r.bootstrap.Fetch(ctx, item.Ref)
		if err != nil {
			return fmt.Errorf("读取 Bootstrap Inventory 制品 %s@%s/%s: %w", item.Ref.PluginID, item.Ref.Version, item.Ref.Channel, err)
		}
		verified, err := r.verifier.Verify(item.Ref, envelope)
		if err != nil {
			return fmt.Errorf("验证 Bootstrap Inventory 制品 %s: %w", item.Ref.PluginID, err)
		}
		if verified.Artifact().SHA256 != item.SHA256 {
			return fmt.Errorf("Bootstrap Inventory 制品 %s 的 SHA-256 不匹配", item.Ref.PluginID)
		}
	}
	return nil
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
	if options.repositoryURL == "" && options.repositoryProfile == "" && options.bootstrapRepository == "" {
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
		resolution.bootstrap = &pluginservice.SignedRepository{Local: local, Trust: trust}
		resolution.sources = append(resolution.sources, resolution.bootstrap)
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
	if options.repositoryProfile != "" {
		profile, err := artifactrepositoryv1.ParseProfileFile(options.repositoryProfile)
		if err != nil {
			return artifactResolution{}, err
		}
		if profile.Protocol != artifactrepositoryv1.ProtocolLocalTest {
			return artifactResolution{}, errors.New("-repository-profile 当前只接受 local-test.v1；remote 继续使用 -repository-url")
		}
		token, err := localtest.ReadTokenFile(options.repositoryTokenFile)
		if err != nil {
			return artifactResolution{}, err
		}
		registry := artifactrepository.NewRegistry()
		if err := registry.Register(artifactrepositoryv1.ProtocolLocalTest, localtest.Factory(token)); err != nil {
			return artifactResolution{}, err
		}
		adapter, err := registry.Open(profile)
		if err != nil {
			return artifactResolution{}, err
		}
		resolution.sources = append(resolution.sources, protocolArtifactSource{adapter: adapter})
	}
	return resolution, nil
}

type protocolArtifactSource struct{ adapter artifactrepository.Adapter }

func (s protocolArtifactSource) Fetch(ctx context.Context, ref pluginv1.ArtifactRef) (artifacttrust.Envelope, error) {
	return s.adapter.ReadExact(ctx, ref)
}

func (s protocolArtifactSource) SourceName() string { return s.adapter.Profile().Protocol }

type nodeControlPlane struct {
	source                nodeagent.DesiredStateSource
	stateStore            nodeagent.StateStore
	router                *addressing.Router
	transport             *addressing.TransportSecurity
	buckets               controlplane.Buckets
	closeNATS             func()
	catalogPublisherKV    jetstream.KeyValue
	closeCatalogPublisher func()
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
		plane.buckets, err = controlplane.EnsureBucketsWithOptions(openCtx, js, controlplane.EnsureBucketsOptions{
			Replicas: options.natsReplicas, Storage: jetstream.FileStorage, SharedStateCapacity: options.sharedStateCapacity,
		})
	} else {
		plane.buckets, err = controlplane.OpenBuckets(openCtx, js)
	}
	if err != nil {
		_ = plane.Close() // 初始化尚未交给调用方，优先返回 bucket 失败。
		return nil, err
	}
	if options.backendPlatformCatalog != "" {
		publisherConnection, connectErr := controlplane.ConnectWithConfig(controlplane.ConnectionConfig{
			URL: options.natsURL, ClientName: "vastplan-catalog-publisher-" + options.nodeID,
			CAFile: options.natsCA, CertFile: options.natsCert, KeyFile: options.natsKey, SeedFile: options.catalogPublisherNATSSeed,
			Insecure: options.natsAllowInsecure, Logf: logf,
		})
		if connectErr != nil {
			_ = plane.Close()
			return nil, fmt.Errorf("连接 Backend Platform Catalog Publisher: %w", connectErr)
		}
		plane.closeCatalogPublisher = publisherConnection.Close
		publisherJS, jsErr := jetstream.New(publisherConnection)
		if jsErr != nil {
			_ = plane.Close()
			return nil, fmt.Errorf("创建 Backend Platform Catalog Publisher JetStream 客户端: %w", jsErr)
		}
		plane.catalogPublisherKV, jsErr = publisherJS.KeyValue(openCtx, controlplane.BackendPlatformCatalogsBucket)
		if jsErr != nil {
			_ = plane.Close()
			return nil, fmt.Errorf("打开 Backend Platform Catalog Publisher bucket: %w", jsErr)
		}
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
	if p != nil && p.closeCatalogPublisher != nil {
		p.closeCatalogPublisher()
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
	if options.bootstrapUpgrade {
		logf("Bootstrap 仓库自升级已启用 inventory=%s", options.bootstrapInventory)
	}
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
