# VastPlan Deployment Manager

`com.vastplan.platform.infrastructure.deployment-manager` 是平台基础插件，负责：

- 保存租户隔离的 Linux 节点定义；
- 以 CAS 管理节点计划版本；
- 管理 `Pending → Approved → Connecting → SystemdActive/Failed/Expired` 首次引导作业；
- 强制申请人与审批人分离；
- 重启时把未确认的 `Connecting/Installing` 作业标记为 `Failed`，禁止自动重复执行；
- 向可信宿主提交类型化 `kernel.node.bootstrap` 请求。

插件只保存 Credential 名称，不保存、读取或返回 SSH 私钥、known_hosts、NATS 身份、制品令牌等 material。`kernel.node.bootstrap` 只由 Backend Kernel 注册，负责通过 CredentialBroker 使用凭证并执行固定 Linux/SSH/systemd Provider。

当前状态文件由部署配置 `VASTPLAN_DEPLOYMENT_MANAGER_STATE_FILE` 指定，必须是规范绝对路径；状态目录不得被 group/other 写入，文件按 `0600` 原子写入并同步目录。生产多节点部署依赖该插件的 leader/fencing 语义，不能把同一状态文件同时挂载给多个非受控实例。

运行该插件的 Node Agent 必须配置 `-credential-root /secure/vastplan-credentials`。目录布局固定为 `<root>/<tenant>/<credential-name>`；根目录和租户目录不能被 group/other 写入，material 文件必须为 `0600`。未配置时宿主不会注册 `kernel.node.bootstrap`。
