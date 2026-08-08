package controlplanecommand

import (
	"flag"
	"fmt"
	"os"

	"cdsoft.com.cn/VastPlan/extensions/libraries/go/sharedstate"
)

type controlPlaneFlagValues struct {
	natsURL                    *string
	natsCA                     *string
	natsCert                   *string
	natsKey                    *string
	natsSeed                   *string
	natsAllowInsecure          *bool
	platformProfilePath        *string
	applicationPath            *string
	backendCatalogPath         *string
	deploymentRevision         *uint64
	allowDevelopmentPlugins    *bool
	key                        *string
	controllerMode             *bool
	controllerID               *string
	repositoryRoot             *string
	repositoryURL              *string
	repositoryProfile          *string
	repositoryTrust            *string
	repositoryToken            *string
	repositoryTokenFile        *string
	repositoryCA               *string
	bootstrap                  *bool
	bootstrapUnitRelease       *bool
	replicas                   *int
	sharedStateMaxBytes        *int64
	sharedStateWarningPercent  *int
	sharedStateCriticalPercent *int
}

func defaultControllerID(value *string) {
	if value == nil || *value != "" {
		return
	}
	hostname, _ := os.Hostname()
	*value = fmt.Sprintf("%s-%d", hostname, os.Getpid())
}

func bindControlPlaneFlags(flags *flag.FlagSet) controlPlaneFlagValues {
	var options controlPlaneFlagValues
	options.natsURL = flags.String("nats-url", "tls://127.0.0.1:4222", "NATS URL")
	options.natsCA = flags.String("nats-ca", "", "NATS CA PEM")
	options.natsCert = flags.String("nats-cert", "", "NATS mTLS 客户端证书 PEM")
	options.natsKey = flags.String("nats-key", "", "NATS mTLS 客户端私钥 PEM")
	options.natsSeed = flags.String("nats-seed", "", "bootstrap 或 controller 角色 NKey seed（0600）")
	options.natsAllowInsecure = flags.Bool("nats-allow-insecure", false, "仅本地开发：允许明文匿名 NATS")
	options.platformProfilePath = flags.String("platform-profile", "", "显式 bootstrap/apply 使用的种子 Platform Profile v1")
	options.applicationPath = flags.String("application-composition", "", "显式 bootstrap/apply 使用的种子 Application Composition v1")
	options.backendCatalogPath = flags.String("backend-platform-catalog", "", "平台签发的 Backend Platform Catalog；controller 为全部预授权目标持续调度")
	options.deploymentRevision = flags.Uint64("deployment-revision", 0, "Resolver 输出的独立单调 Deployment revision")
	options.allowDevelopmentPlugins = flags.Bool("allow-development-plugins", false, "仅本地开发：允许 example 或历史未分类首方插件")
	options.key = flags.String("key", "", "KV key；默认从 metadata.tenant/name 生成")
	options.controllerMode = flags.Bool("controller", false, "持续 watch v2 部署与节点租约并生成每节点 assignment")
	options.controllerID = flags.String("controller-id", "", "controller 选主身份；默认 hostname-pid")
	options.repositoryRoot = flags.String("repository", ".vastplan/repository", "controller 读取完整 manifest 的本地不可变制品仓库")
	options.repositoryURL = flags.String("repository-url", "", "controller 使用的 HTTPS 托管制品仓库；本地 Seed 缺失时后备")
	options.repositoryProfile = flags.String("repository-profile", "", "controller 使用的精确仓库协议 Profile；当前用于 local-test.v1")
	options.repositoryTrust = flags.String("repository-trust", "", "controller 远端制品发布者信任文档")
	options.repositoryToken = flags.String("repository-token", "", "controller 远端仓库读令牌；默认读取 VASTPLAN_ARTIFACT_READ_TOKEN")
	options.repositoryTokenFile = flags.String("repository-token-file", "", "controller local-test owner-only 访问令牌文件")
	options.repositoryCA = flags.String("repository-ca", "", "controller 远端制品仓库自定义 CA PEM")
	options.bootstrap = flags.Bool("bootstrap", false, "创建/校准控制面 bucket")
	options.bootstrapUnitRelease = flags.Bool("bootstrap-unit-release", false, "可信宿主受限通道：只允许现有 Bootstrap 单元的插件精确换版")
	options.replicas = flags.Int("replicas", 1, "bootstrap 时的 JetStream 副本数；生产建议至少 3")
	options.sharedStateMaxBytes = flags.Int64("shared-state-max-bytes", 0, "bootstrap 时 Shared State 硬上限；生产必须显式配置")
	options.sharedStateWarningPercent = flags.Int("shared-state-warning-percent", sharedstate.DefaultWarningPercent, "Shared State 容量 warning 阈值百分比")
	options.sharedStateCriticalPercent = flags.Int("shared-state-critical-percent", sharedstate.DefaultCriticalPercent, "Shared State 容量 critical 阈值百分比")
	return options
}
