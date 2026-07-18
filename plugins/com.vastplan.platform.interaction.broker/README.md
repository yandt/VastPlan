# Interaction Broker

`com.vastplan.platform.interaction.broker` 是平台级基础插件：它持久化并裁决跨 Portal、Mobile 与 Runner 的人机交互任务。

- 发起方只能通过 `platform.interaction-broker/open` 创建与其可信调用身份同名的来源任务；
- 呈现端只能读取、呈现和响应本租户中明确授权给自身或其角色的任务；
- `respond` 是一次性终态写入；并发响应最多一个成功；
- `secretRef` 字段只接受 `credentialRefs`，拒绝把秘密内容写入交互状态或审计；
- 状态文件由宿主的 `kernel.config.get` 提供 `platform.interaction-broker.stateFile`，插件进程不从环境变量取得其位置。

当前实现提供 `open/list/get/present/respond/cancel` 持久化闭环。Runner 的长连接 `watch` 与 Portal Edge/Mobile Gateway 的传输适配会在下一阶段接入，服务本身不依赖任何浏览器或原生 UI 运行时。
