package main

import (
	"errors"
	"flag"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"cdsoft.com.cn/VastPlan/core/kernels/backend/nodeagent"
	"cdsoft.com.cn/VastPlan/core/shared/go/controlplane"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/sharedstate"
)

type reconcileOptions struct {
	desiredPath, startupFile, repositoryRoot, repositoryURL, repositoryTrust, repositoryToken, repositoryCA string
	repositoryProfile, repositoryTokenFile                                                                  string
	bootstrapRepository                                                                                     string
	bootstrapInventory                                                                                      string
	runtimeRoot, actualPath, lockPath, nodeID, labelsRaw                                                    string
	credentialRoot                                                                                          string
	backendPlatformCatalog                                                                                  string
	frontendDeliveryOrigin                                                                                  string
	firstPartyPublishers                                                                                    string
	thirdPartyPluginPolicy, publisherPluginPolicies                                                         string
	defaultPluginContextAccess, publisherPluginContextAccess                                                string
	defaultPluginKernelServices, publisherPluginKernelServices                                              string
	pluginPlacementDefault, publisherPluginPlacements, pluginPlacements                                     string
	runtimeHostingDefault, publisherRuntimeHosting, pluginRuntimeHosting                                    string
	capacityCPU, capacityMemory, capacityGPU                                                                int64
	interval                                                                                                time.Duration
	natsURL, natsCA, natsCert, natsKey, natsSeed, catalogPublisherNATSSeed, transportSeed, transportTrust   string
	natsAllowInsecure, natsBootstrap, allowDevelopmentPlugins                                               bool
	bootstrapUpgrade                                                                                        bool
	publishBootstrapReferences                                                                              bool
	requireThirdPartyIsolation                                                                              bool
	executionPolicy                                                                                         nodeagent.ExecutionPolicy
	contextPolicy                                                                                           nodeagent.ContextPolicy
	grantPolicy                                                                                             nodeagent.KernelServiceGrantPolicy
	placementPolicy                                                                                         nodeagent.PlacementPolicy
	hostingPolicy                                                                                           nodeagent.RuntimeHostingPolicy
	desiredKey, assignmentKey, deploymentName, deploymentTenant                                             string
	natsReplicas                                                                                            int
	sharedStateMaxBytes                                                                                     int64
	sharedStateWarningPercent, sharedStateCriticalPercent                                                   int
	sharedStateCapacity                                                                                     sharedstate.CapacityPolicy
}

func parseReconcileOptions(args []string) (reconcileOptions, error) {
	var options reconcileOptions
	flags := newReconcileFlagSet(&options)
	if err := flags.Parse(args); err != nil {
		return reconcileOptions{}, err
	}
	visited := map[string]bool{}
	flags.Visit(func(item *flag.Flag) { visited[item.Name] = true })
	return finalizeReconcileOptions(options, visited)
}

func newReconcileFlagSet(options *reconcileOptions) *flag.FlagSet {
	flags := flag.NewFlagSet("reconcile", flag.ContinueOnError)
	flags.StringVar(&options.desiredPath, "desired", "", "本地 DesiredState v1 配置文件（JSON/YAML）")
	flags.StringVar(&options.startupFile, "startup-file", "", "本地启动配置文件（JSON/YAML），等价于 -desired")
	flags.StringVar(&options.repositoryRoot, "repository", ".vastplan/repository", "本地插件制品仓库")
	flags.StringVar(&options.repositoryURL, "repository-url", "", "HTTPS 远端签名制品仓库；设置后替代本地仓库")
	flags.StringVar(&options.repositoryProfile, "repository-profile", "", "精确仓库协议 Profile JSON；当前用于 local-test.v1")
	flags.StringVar(&options.repositoryTokenFile, "repository-token-file", "", "local-test owner-only 访问令牌文件")
	flags.StringVar(&options.repositoryTrust, "repository-trust", "", "远端制品发布者信任文档")
	flags.StringVar(&options.repositoryToken, "repository-token", "", "远端制品读令牌；默认读取 VASTPLAN_ARTIFACT_READ_TOKEN")
	flags.StringVar(&options.repositoryCA, "repository-ca", "", "远端制品仓库自定义 CA PEM")
	flags.StringVar(&options.bootstrapRepository, "bootstrap-repository", "", "预置签名种子仓库；精确命中时优先于远端源")
	flags.StringVar(&options.bootstrapInventory, "bootstrap-inventory", "", "root-owned Bootstrap Inventory（Seed/LKG 精确引用与单调 generation）")
	flags.BoolVar(&options.bootstrapUpgrade, "bootstrap-upgrade", false, "由可信宿主事务式镜像仓库关键插件并在健康后推进 LKG")
	flags.BoolVar(&options.publishBootstrapReferences, "publish-bootstrap-references", false, "由本节点以信任文档授权的 SYSTEM 子身份发布 Seed/LKG 引用")
	flags.StringVar(&options.runtimeRoot, "runtime-root", ".vastplan/runtime/plugins", "内容寻址安装目录")
	flags.StringVar(&options.actualPath, "actual-state", ".vastplan/runtime/actual-state.json", "实际态报告文件")
	flags.StringVar(&options.lockPath, "lock", "", "单实例锁文件；默认 <actual-state>.lock")
	flags.StringVar(&options.nodeID, "node-id", "local", "当前节点 ID")
	flags.StringVar(&options.labelsRaw, "labels", "", "节点标签，逗号分隔 key=value")
	flags.StringVar(&options.credentialRoot, "credential-root", "", "可信凭证挂载根目录：<root>/<tenant>/<credential-name>；留空不启用节点引导 Broker")
	flags.StringVar(&options.backendPlatformCatalog, "backend-platform-catalog", "", "平台签发的 Backend Platform Catalog；配置后向 deployment-manager 开放在线编排")
	flags.StringVar(&options.frontendDeliveryOrigin, "frontend-delivery-origin", "", "可信 Portal 前端快照中央物化目录；仅在承载 Portal Composer 的节点配置")
	flags.StringVar(&options.thirdPartyPluginPolicy, "third-party-plugin-policy", string(nodeagent.PublisherPolicyRequireIsolation), "未单独配置发布者时的策略: require-isolation, allow-trusted, deny")
	flags.StringVar(&options.publisherPluginPolicies, "publisher-plugin-policies", "", "发布者级策略，逗号分隔 publisher=policy；优先于全局策略")
	flags.StringVar(&options.defaultPluginContextAccess, "default-plugin-context-access", "", "未知发布者的 CallContext 字段上限，逗号分隔；空值使用安全默认")
	flags.StringVar(&options.publisherPluginContextAccess, "publisher-plugin-context-access", "", "发布者级 CallContext 上限，分号分隔 publisher=field,field；* 表示全部已知字段")
	flags.StringVar(&options.defaultPluginKernelServices, "default-plugin-kernel-services", "", "未知发布者的 Kernel Service 上限，逗号分隔；空值默认不授予")
	flags.StringVar(&options.publisherPluginKernelServices, "publisher-plugin-kernel-services", "", "发布者级 Kernel Service 上限，分号分隔 publisher=service,service；* 表示全部已注册服务")
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
	flags.StringVar(&options.catalogPublisherNATSSeed, "catalog-publisher-nats-seed", "", "独立 Backend Platform Catalog publisher NKey seed 文件（0600）")
	flags.StringVar(&options.transportSeed, "transport-seed", "", "addressing 传输身份 NKey seed 文件（0600）")
	flags.StringVar(&options.transportTrust, "transport-trust", "", "addressing 传输身份信任文档 JSON")
	flags.BoolVar(&options.natsAllowInsecure, "nats-allow-insecure", false, "仅本地开发：允许明文匿名 NATS")
	flags.BoolVar(&options.allowDevelopmentPlugins, "allow-development-plugins", false, "仅本地开发：允许在线组合使用 example 或历史未分类首方插件")
	flags.StringVar(&options.desiredKey, "desired-key", controlplane.DesiredKey("", "local-development"), "NATS DesiredState key")
	flags.BoolVar(&options.natsBootstrap, "nats-bootstrap", false, "创建/校准控制面 KV bucket（仅初始化/开发使用）")
	flags.IntVar(&options.natsReplicas, "nats-replicas", 1, "初始化 KV bucket 的 JetStream 副本数；生产建议至少 3")
	flags.Int64Var(&options.sharedStateMaxBytes, "shared-state-max-bytes", 0, "初始化 Shared State 的硬上限；生产 bootstrap 必须显式配置")
	flags.IntVar(&options.sharedStateWarningPercent, "shared-state-warning-percent", sharedstate.DefaultWarningPercent, "Shared State warning 阈值百分比")
	flags.IntVar(&options.sharedStateCriticalPercent, "shared-state-critical-percent", sharedstate.DefaultCriticalPercent, "Shared State critical 阈值百分比")
	flags.StringVar(&options.assignmentKey, "assignment-key", "", "节点级 assignment key；设置后从 ASSIGNMENTS_V1 消费，覆盖 -desired-key")
	flags.StringVar(&options.deploymentName, "deployment", "", "集群 Deployment v2 名称；自动生成当前节点 assignment key")
	flags.StringVar(&options.deploymentTenant, "tenant", "", "集群 Deployment v2 租户；与 -deployment 一起使用")
	return flags
}

func finalizeReconcileOptions(options reconcileOptions, visited map[string]bool) (reconcileOptions, error) {
	if options.startupFile != "" {
		if options.desiredPath != "" {
			return reconcileOptions{}, errors.New("-startup-file 与 -desired 不能同时设置")
		}
		options.desiredPath = options.startupFile
	}
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
	options.grantPolicy, err = nodeagent.ParseKernelServiceGrantPolicy(options.defaultPluginKernelServices, options.publisherPluginKernelServices)
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
	if options.catalogPublisherNATSSeed != "" && options.backendPlatformCatalog == "" {
		return reconcileOptions{}, errors.New("-catalog-publisher-nats-seed 只能与 -backend-platform-catalog 同时配置")
	}
	if options.backendPlatformCatalog != "" && !options.natsAllowInsecure && options.catalogPublisherNATSSeed == "" {
		return reconcileOptions{}, errors.New("生产在线 Profile 激活必须配置独立 -catalog-publisher-nats-seed")
	}
	if options.natsAllowInsecure && options.catalogPublisherNATSSeed != "" {
		return reconcileOptions{}, errors.New("本地 insecure NATS 不得混用 catalog-publisher NKey seed")
	}
	if options.natsBootstrap {
		options.sharedStateCapacity, err = sharedstate.ResolveCapacityPolicy(
			options.sharedStateMaxBytes, options.sharedStateWarningPercent, options.sharedStateCriticalPercent, options.natsAllowInsecure,
		)
		if err != nil {
			return reconcileOptions{}, err
		}
	} else if options.sharedStateMaxBytes != 0 {
		return reconcileOptions{}, errors.New("-shared-state-max-bytes 只能与 -nats-bootstrap 同时配置")
	}
	if options.frontendDeliveryOrigin != "" && (!filepath.IsAbs(options.frontendDeliveryOrigin) || filepath.Clean(options.frontendDeliveryOrigin) != options.frontendDeliveryOrigin) {
		return reconcileOptions{}, errors.New("-frontend-delivery-origin 必须是规范绝对路径")
	}
	if options.lockPath == "" {
		options.lockPath = options.actualPath + ".lock"
	}
	if options.credentialRoot != "" && (!filepath.IsAbs(options.credentialRoot) || filepath.Clean(options.credentialRoot) != options.credentialRoot) {
		return reconcileOptions{}, errors.New("-credential-root 必须是规范绝对路径")
	}
	if options.bootstrapInventory != "" && (!filepath.IsAbs(options.bootstrapInventory) || filepath.Clean(options.bootstrapInventory) != options.bootstrapInventory) {
		return reconcileOptions{}, errors.New("-bootstrap-inventory 必须是规范绝对路径")
	}
	if options.bootstrapInventory != "" && options.bootstrapRepository == "" {
		return reconcileOptions{}, errors.New("-bootstrap-inventory 必须与 -bootstrap-repository 同时配置")
	}
	if options.repositoryURL != "" && options.repositoryProfile != "" {
		return reconcileOptions{}, errors.New("-repository-url 与 -repository-profile 不能同时配置")
	}
	if options.repositoryProfile == "" && options.repositoryTokenFile != "" || options.repositoryProfile != "" && options.repositoryTokenFile == "" {
		return reconcileOptions{}, errors.New("-repository-profile 与 -repository-token-file 必须同时配置")
	}
	if options.repositoryProfile != "" && (!filepath.IsAbs(options.repositoryProfile) || filepath.Clean(options.repositoryProfile) != options.repositoryProfile || !filepath.IsAbs(options.repositoryTokenFile) || filepath.Clean(options.repositoryTokenFile) != options.repositoryTokenFile) {
		return reconcileOptions{}, errors.New("Repository Profile 与 token file 必须是规范绝对路径")
	}
	if options.repositoryProfile != "" && (options.repositoryToken != "" || options.repositoryCA != "") {
		return reconcileOptions{}, errors.New("local-test Profile 不得混用 remote token 或 CA 参数")
	}
	repositoryConfigured := options.repositoryURL != "" || options.repositoryProfile != ""
	if repositoryConfigured && options.bootstrapRepository != "" && options.bootstrapInventory == "" {
		return reconcileOptions{}, errors.New("托管仓库与 Seed 后备源并用时必须提供 -bootstrap-inventory")
	}
	if options.publishBootstrapReferences && (options.bootstrapInventory == "" || options.natsURL == "") {
		return reconcileOptions{}, errors.New("-publish-bootstrap-references 必须同时配置 Bootstrap Inventory 与 NATS 控制面")
	}
	if options.bootstrapUpgrade && (options.bootstrapInventory == "" || options.bootstrapRepository == "" || !repositoryConfigured || !options.publishBootstrapReferences) {
		return reconcileOptions{}, errors.New("-bootstrap-upgrade 必须同时配置 Bootstrap Inventory、Seed 仓库、远端候选仓库与引用发布职责")
	}
	if options.natsURL != "" && options.assignmentKey != "" {
		assignmentNodeID, err := controlplane.AssignmentKeyNodeID(options.assignmentKey)
		if err != nil || assignmentNodeID != options.nodeID {
			return reconcileOptions{}, fmt.Errorf("assignment key 不属于当前节点 %s", options.nodeID)
		}
	}
	return options, nil
}
