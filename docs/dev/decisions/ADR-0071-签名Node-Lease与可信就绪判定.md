# ADR-0071 签名 Node Lease 与可信就绪判定

- 状态：已采纳
- 日期：2026-07-18
- 关联：[ADR-0025](ADR-0025-NATS控制面寻址与多节点调度.md)、[ADR-0027](ADR-0027-NATS生产安全与最小权限.md)、[ADR-0069](ADR-0069-SSH首次引导与Node-Agent接管.md)、[ADR-0070](ADR-0070-Deployment-Manager与可信引导执行边界.md)

## 背景

SSH 返回和 systemd `active` 只能证明远端进程已启动，不能证明 Node Agent 已用预期租户、Deployment 和 addressing 传输身份加入控制面。原 Node Lease 只包含 `node_id/labels/capacity`，如果直接据此推进 `Ready`，同名节点、错误作用域或被替换的 Lease 都可能造成误判。

## 决策

1. Node Lease 升级为 v3，必须包含 cluster-global `node_id`、tenant、Deployment、容量、更新时间和 addressing 传输公钥。
2. Node Agent 使用自身 addressing NKey 对完整 Lease 记录签名。签名 subject 同时绑定 tenant、Deployment 和 node ID；观察者从传输信任文档验证签名、公钥、角色、tenant 与 node ID。
3. Lease KV key 使用 `tenant + deployment + node` 作用域。NATS `node` 与 `manager-node` 身份必须在服务端 ACL 中绑定同一作用域，只能写自身精确 Lease key；NATS 安全配置拒绝重复 cluster-global node ID。
4. 新增窄化内核能力 `kernel.node.readiness`。它只接受认证后的 Deployment Manager 插件，读取 Node Lease、验证签名与新鲜度，并返回封闭结果 `Waiting/Ready/Rejected`；插件不能获得 KV、NATS 连接或信任文档。
5. 普通 `node` 身份不能读取其他节点 Lease。承载 Deployment Manager 的管理节点使用 `manager-node` NATS 身份，它只比 node 增加 Node Lease 读取权，不获得 Deployment/Assignment 全局写权限。
6. Deployment Manager 在引导完成及查询 Bootstrap Job 时执行拉式收敛：精确匹配推进 `SystemdActive → Ready`，签名或身份不匹配进入 `Failed/readiness_rejected`，超过作业期限进入 `Failed/readiness_timeout`。观察服务暂时不可用时保持 `SystemdActive`，不得误判失败或 Ready。
7. Controller 调度只接受 v3 Lease，且只选择 tenant 与 Deployment 都匹配的节点。Lease key 与记录声明不一致时 fail-closed。
8. 后续可把相同观察器接到持久 Node Lease 领域事件，实现无需 UI 轮询的主动推进；事件源只能替换触发方式，不能绕过本 ADR 的签名与作用域校验。

## 备选方案

- **只检查 node ID**：改动最小，但无法区分租户、Deployment 和传输身份；拒绝。
- **Deployment Manager 直接读取 NATS KV**：插件会获得控制面凭证和存储耦合；拒绝。
- **systemd active 直接视为 Ready**：无法证明 Node Agent 已接管；拒绝。
- **首期即建立完整事件投影**：长期更主动，但会同时引入持久消费、幂等和恢复机制；本期保留兼容事件化的窄观察接口，先完成安全闭环。

## 影响

- 首次引导计划必须声明非秘密 `transportPublicKey`，并与实际 transport trust 身份一致。
- `natssecurity` 生成器要求 tenant 与 Deployment，并默认生成独立 `manager-node` 身份。
- Node Lease v1/v2 不再进入调度或 Ready 判定；项目仍在开发阶段，不提供旧 Lease 数据迁移。
