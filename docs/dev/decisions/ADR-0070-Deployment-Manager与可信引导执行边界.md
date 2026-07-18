# ADR-0070 Deployment Manager 与可信引导执行边界

- 状态：已采纳
- 日期：2026-07-18
- 关联：[ADR-0036](ADR-0036-Backend核心SPI边界.md)、[ADR-0068](ADR-0068-分布式平台管理中心与强类型BFF.md)、[ADR-0069](ADR-0069-SSH首次引导与Node-Agent接管.md)

## 背景

平台管理中心需要保存 Linux 节点定义、发起首次引导并记录审批状态。如果 Deployment Manager 插件直接读取 SSH 私钥或执行 Shell，则插件漏洞会获得主机控制权；如果 Portal Edge 直接执行 SSH，则 BFF 会长期持有基础设施凭证，并绕过插件状态、权限检查和集群 leader 语义。

## 决策

1. 新增平台基础插件 `com.vastplan.platform.infrastructure.deployment-manager`。它只保存租户隔离的非敏感 `nodebootstrap.Plan`、Credential 名称、节点 CAS 版本和 Bootstrap Job，不保存或返回 material。
2. Bootstrap Job 使用 `Pending/Approved/Connecting/Installing/SystemdActive/Ready/Failed/Expired` 封闭状态集。首期同步执行到 `SystemdActive`；`Ready` 必须等待控制面观察到匹配 Node Lease，不能由 SSH 成功直接推导。
3. `createBootstrap` 与 `approveBootstrap` 使用独立角色，并在插件领域层再次强制请求人与审批人不同。Edge 和权限插件的角色检查只是纵深防御，不能替代该职责分离规则。
4. 插件只能调用唯一的 `kernel.node.bootstrap`。宿主只接受已认证的精确 Deployment Manager 插件 ID，重新校验 tenant 与完整计划，再交给注入的 `nodebootstrap.Broker`。
5. `nodebootstrapbroker` 通过 `kernelspi.CredentialBroker.WithCredential` 在嵌套回调生命周期内使用 SSH identity、known_hosts 和节点身份材料；只生成固定 Linux/SSH/systemd 脚本。插件和 BFF 均不能选择命令、参数或本地文件路径。
6. Portal Edge 只暴露白名单节点与 Bootstrap Job HTTP 资源，不提供通用 capability 代理。TypeScript SDK 复用同一强类型契约。
7. Broker 是内核依赖，缺失时不注册 `kernel.node.bootstrap` 并 fail-closed。首期生产适配器通过 `-credential-root` 读取企业 secret mount 的 `<root>/<tenant>/<credential-name>` 0600 文件；不得为了“开箱即用”退回环境变量明文或插件读取凭证。
8. Docker、Kubernetes、Ansible 和云 Provider 暂不进入首期。未来 Provider 必须实现相同类型化 Broker 语义，不得扩大为任意 Shell SPI。
9. 高权限执行采用“未知结果不自动重放”：进程重启发现 `Connecting/Installing` 时持久化为 `Failed`，错误码为 `platform.deployment.interrupted`。因为远端可能已完成，自动重试可能重复产生副作用；后续由操作者核对节点状态并重新申请幂等引导。

## 备选方案

- **Deployment Manager 直接 SSH**：实现简单，但插件可接触全部主机凭证并绕过内核强制点；拒绝。
- **Portal Edge 直接 SSH**：缩短调用链，却让互联网入口持有基础设施高权限；拒绝。
- **先定义通用 Provider/Shell SPI**：扩展面过大且难以证明安全；本期只保留类型化 Broker 接口。

## 影响

- 节点定义、审批与执行形成双层授权和可恢复状态记录。
- 当前代码已具备插件、BFF、角色、Broker SPI、目录 CredentialBroker、Linux SSH Broker，以及由 [ADR-0071](ADR-0071-签名Node-Lease与可信就绪判定.md) 定义的 Node Lease 到 `Ready` 可信观察；集中式 Vault/KMS material provider 仍待实现。
- 服务 Draft、Resolver 发布、管理页面和多 Provider 仍是后续纵切，不属于本 ADR 已完成范围。
