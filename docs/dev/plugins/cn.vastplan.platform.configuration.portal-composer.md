# Portal Composer

插件 ID：`cn.vastplan.platform.configuration.portal-composer`

当前制品版本：`1.1.0`

该平台基础插件以 `leader + leader-owned + cluster` 方式治理 Portal Application、Platform Profile、PortalBinding 和不可变 Activation。发布输入只代表可选择；只有通过可信 Portal Edge 校验、物化并完成 `expectedCurrentId` CAS 的 Activation 才是线上事实。

主要能力：

- Application/Profile/Binding 分域草稿、异人审批与发布；
- Portal Activation、历史精确回滚和 Frontend Test Release；
- 只通过 `kernel.portal.catalog.*` 窄服务取得可信校验和已验签包的精确引用，不接触仓库令牌、签名密钥或制品字节；
- 激活前发布“旧活动 + 新候选”引用并集，激活后先保护回滚历史、再收敛活动精确引用；
- 持久化 `referencePending` outbox 在管理读取及控制器重启后幂等重试；
- Frontend Test Release 在候选验证和激活前发布独立的精确 `artifact-lock`，仓库不可用时 fail-closed。

Portal Edge 通过 `kernel.portal.artifact-references.publish` 将已密封快照路由到集群仓库。该服务只接受经宿主认证的 Composer 插件、当前租户，以及 `portal/*` owner 命名空间；它不是通用 capability 代理。仅 `allow-unsigned-local` 且没有集群仓库时可使用内存校验发布器，生产环境缺少仓库路由会拒绝 Activation。

状态格式为 v4。状态只保存治理数据、精确制品引用与 outbox 标记，不保存任何凭证 material。完整 Portal 边界见《[前端门户内核](../architecture/前端门户内核.md)》、《[制品仓库与测试发布](../architecture/制品仓库与测试发布.md)》和 [ADR-0100](../decisions/ADR-0100-制品生命周期引用保护与垃圾回收.md)。
