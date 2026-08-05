# Interaction Broker

当前制品版本：`0.2.0`

`cn.vastplan.platform.interaction.broker` 是平台级基础插件：它持久化并裁决跨 Portal、Mobile 与 Runner 的人机交互任务。

- 发起方只能通过 `platform.interaction-broker/open` 创建与其可信调用身份同名的来源任务；
- 呈现端只能读取、呈现和响应本租户中明确授权给自身或其角色的任务；
- `respond` 是一次性终态写入；并发响应最多一个成功；
- `secretRef` 字段只接受 `credentialRefs`，拒绝把秘密内容写入交互状态或审计；
- 每条交互以 `platform.interaction.record` DataModel 独立写入平台控制数据库，业务状态转换通过 Repository CAS 竞争唯一终态；插件不再读取或写入本机状态文件。
- 插件只依赖 `foundation.data.record-store@^1.1.0`，看不到平台控制库连接、驱动、账号或密码；租户字段由可信调用上下文注入。

当前实现提供 `open/list/get/watch/present/respond/cancel` 持久化闭环。`watch` 使用最后一次 `updatedAt` 作为游标，Runner/后端来源可在断线重连后恢复观察；Node Portal Kernel 传输适配已接入，Mobile Gateway/Native Adapter 仍待实现。服务本身不依赖任何浏览器或原生 UI 运行时。
