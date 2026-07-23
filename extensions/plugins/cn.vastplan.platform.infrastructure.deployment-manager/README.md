# VastPlan Deployment Manager

`cn.vastplan.platform.infrastructure.deployment-manager` 是平台基础插件，负责：

- 保存租户隔离的 Linux 节点定义；
- 以 CAS 管理节点计划版本；
- 管理 `Pending → Approved → Connecting → SystemdActive → Ready/Failed` 首次引导作业；
- 强制申请人与审批人分离；
- 重启时把未确认的 `Connecting/Installing` 作业标记为 `Failed`，禁止自动重复执行；
- 向可信宿主提交类型化 `kernel.node.bootstrap` 请求；
- 通过窄化的 `kernel.node.readiness` 观察签名 Node Lease，并在引导完成或查询作业时收敛最终状态。
- 列出平台预授权的 Backend 部署目标；
- 管理 Application Composition 草稿、异人审批、发布审计和单调 revision 回滚；
- 为 plugin-settings 创建 candidate 绑定的 Application 配置修订，禁止普通发布入口绕过候选凭证准备，并在 readiness 失败时自动发布回滚 revision；
- 为 Platform Profile restart 配置持久化独立 Saga：调用窄内核端口准备候选、执行异人审批、激活 Catalog、发布 Deployment、等待精确 readiness，并在失败时依次生成单调 Catalog/Deployment 回滚修订；
- 通过 `kernel.deployment.preview/publish` 请求可信内核选择固定 Platform Profile、验签制品并 CAS 发布 Deployment v2；
- 以 CAS 保存 Backend 应用插件的 `TestTargetBinding`，精确复核 testing Catalog ref、摘要、publisher 与 repository revision；
- 通过 `kernel.deployment.readiness` 等待候选修订真实收敛，失败时以新的单调 revision 自动恢复上一组合；
- 重启时把未完成测试发布落为 `Failed + rollbackRequired`，由 `rollbackTestRelease` 显式、安全地完成恢复，不盲目重放；
- 提供 Portal 动态表单页面，配置应用插件、服务依赖、replicas、实例策略和节点标签，并展示最终解析预览。

插件只保存 Credential 名称，不保存、读取或返回 SSH 私钥、known_hosts、NATS 身份、制品令牌等 material。`kernel.node.bootstrap` 只由 Backend Kernel 注册，负责通过 CredentialBroker 使用凭证并执行固定 Linux/SSH/systemd Provider。

`kernel.node.readiness` 也只由 Backend Kernel 注册。内核校验预期 tenant、Deployment、cluster-global node ID、transport 公钥、Lease 签名、KV key 与新鲜度，插件只接收 `Waiting/Ready/Rejected`，不获得 NATS 连接、KV 句柄或 transport trust。观察服务暂时不可用时作业保持 `SystemdActive`；身份或签名不匹配进入 `Failed/readiness_rejected`，超过期限进入 `Failed/readiness_timeout`。

在线服务组合只保存 Application Composition。`BackendPlatformCatalog` 的启动文件只是可信 Seed，以 `(tenantId, deploymentName)` 锁定精确 Platform Profile；插件和浏览器都不能读取或修改 Catalog 全文。只有 Deployment Manager 的精确宿主身份可调用 `kernel.platform-profile.*`，而真正的 Profile 克隆、Catalog CAS 和 binding 替换仍在内核。普通草稿在创建和提交时执行可信预览，发布时再次解析并核对已审批摘要。Profile 候选存在时目标 binding 的普通 Application 发布被锁定；中断依靠 candidate/request digest 检查点恢复，不盲目重放副作用。

测试目标绑定只能指向活动 Application Composition 内已有的应用插件，不能增加插件，也不能覆盖 `cn.vastplan.foundation.*` 或 `cn.vastplan.platform.*`。测试发布只接受 `testing` channel 的 SemVer 预发布版本和精确 SHA/repositoryRevision；上传与发布是两个事务。候选就绪与回滚通过 `kernel.deployment.readiness` 读取内核持有的 NATS Composition report，插件不获得 KV 句柄。

当前状态文件由部署配置 `VASTPLAN_DEPLOYMENT_MANAGER_STATE_FILE` 指定，必须是规范绝对路径；状态目录不得被 group/other 写入，文件按 `0600` 原子写入并同步目录。生产多节点部署依赖该插件的 leader/fencing 语义，不能把同一状态文件同时挂载给多个非受控实例。

运行该插件的管理节点必须使用与自身作用域绑定的 `manager-node` NATS 身份，并配置 `-tenant`、`-deployment`、`-node-id`、`-transport-seed` 与 `-transport-trust`。生产在线编排还必须配置 `-backend-platform-catalog /etc/vastplan/backend-platform-catalog.json`，并给该可信内核身份最小 Deployment KV 写权；Controller 进程使用同一 Catalog 的 `controlplane -controller -backend-platform-catalog ...` 为全部目标调度。它还必须配置 `-credential-root /secure/vastplan-credentials`；目录布局固定为 `<root>/<tenant>/<credential-name>`，material 文件必须为 `0600`。缺少任一依赖时相应内核服务不注册并 fail-closed。
