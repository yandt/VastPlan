# VastPlan Deployment Manager

`com.vastplan.platform.infrastructure.deployment-manager` 是平台基础插件，负责：

- 保存租户隔离的 Linux 节点定义；
- 以 CAS 管理节点计划版本；
- 管理 `Pending → Approved → Connecting → SystemdActive → Ready/Failed` 首次引导作业；
- 强制申请人与审批人分离；
- 重启时把未确认的 `Connecting/Installing` 作业标记为 `Failed`，禁止自动重复执行；
- 向可信宿主提交类型化 `kernel.node.bootstrap` 请求；
- 通过窄化的 `kernel.node.readiness` 观察签名 Node Lease，并在引导完成或查询作业时收敛最终状态。

插件只保存 Credential 名称，不保存、读取或返回 SSH 私钥、known_hosts、NATS 身份、制品令牌等 material。`kernel.node.bootstrap` 只由 Backend Kernel 注册，负责通过 CredentialBroker 使用凭证并执行固定 Linux/SSH/systemd Provider。

`kernel.node.readiness` 也只由 Backend Kernel 注册。内核校验预期 tenant、Deployment、cluster-global node ID、transport 公钥、Lease 签名、KV key 与新鲜度，插件只接收 `Waiting/Ready/Rejected`，不获得 NATS 连接、KV 句柄或 transport trust。观察服务暂时不可用时作业保持 `SystemdActive`；身份或签名不匹配进入 `Failed/readiness_rejected`，超过期限进入 `Failed/readiness_timeout`。

当前状态文件由部署配置 `VASTPLAN_DEPLOYMENT_MANAGER_STATE_FILE` 指定，必须是规范绝对路径；状态目录不得被 group/other 写入，文件按 `0600` 原子写入并同步目录。生产多节点部署依赖该插件的 leader/fencing 语义，不能把同一状态文件同时挂载给多个非受控实例。

运行该插件的管理节点必须使用与自身作用域绑定的 `manager-node` NATS 身份，并配置 `-tenant`、`-deployment`、`-node-id`、`-transport-seed` 与 `-transport-trust`。`manager-node` 只比普通 node 增加 Node Lease 读取权，不获得全局 Deployment 或 Assignment 写权。它还必须配置 `-credential-root /secure/vastplan-credentials`；目录布局固定为 `<root>/<tenant>/<credential-name>`，根目录和租户目录不能被 group/other 写入，material 文件必须为 `0600`。未注入 CredentialBroker 时宿主不会注册 `kernel.node.bootstrap`；没有安全 transport 和 Nodes KV 时不会注册 `kernel.node.readiness`。
