# 节点部署管理服务

插件 ID：`com.vastplan.platform.infrastructure.deployment-manager`

该 platform 基础插件以 `leader + leader-owned + cluster + platform routing domain` 运行，持有租户隔离的节点计划、CAS 版本和 Bootstrap Job。它依赖 settings、credentials、artifact repository 与 `kernel.node.bootstrap`，但只保存 Credential 名称，永远不能读取 SSH、NATS 或制品令牌 material。

当前 capability 为 `platform.deployment`：

- `listNodes`、`putNode`：查询或以 CAS 保存节点计划；
- `listBootstrapJobs`：查询首次引导状态；
- `createBootstrap`：由 `platform.deployment.bootstrap` 角色申请；
- `approveBootstrap`：由不同的 `platform.deployment.approve` 用户批准并触发可信宿主。

活动作业期间节点定义被冻结。进程重启时，未确认的 `Connecting/Installing` 会落为 `Failed` 且不自动重放，避免一次高权限 SSH 操作被重复执行；操作者可基于远端实际状态重新申请。SSH/systemd 成功只进入 `SystemdActive`；Node Lease 观察器完成后才能进入 `Ready`。状态文件配置、运行说明见插件目录 [README](../../../extensions/plugins/com.vastplan.platform.infrastructure.deployment-manager/README.md)，完整边界见《[服务部署控制台](../architecture/服务部署控制台.md)》与 [ADR-0070](../decisions/ADR-0070-Deployment-Manager与可信引导执行边界.md)。
