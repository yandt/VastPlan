# VastPlan 后续任务

> 本文件只记录已经明确推迟、且具备充分上下文的跨阶段任务。当前实施范围与架构单一真相源仍以 [`docs/dev/00-index.md`](docs/dev/00-index.md) 为入口。

## Portal 制品快照可达性回收

- **What**：为 Portal 中央交付 Origin 和 Edge cache 建立基于 `PortalActivation` 历史的内容寻址对象可达性回收。
- **Why**：候选物化后可能因治理 CAS 冲突而未激活；历史 Activation 过期后也会留下不再引用的模块和快照。长期只保留会持续占用磁盘。
- **Pros**：自动控制存储容量；保留仍需恢复的历史；避免人工误删被多个 Portal 共用的 digest。
- **Cons**：必须处理共享对象、保留宽限期、多 Edge 读者、删除墓碑、崩溃恢复和并发激活，属于独立的制品生命周期工程。
- **Context**：当前阶段只记录 orphan 的 digest、时间和失败原因，并设置容量与对象数告警；达到硬上限时拒绝新物化但不影响活动 Portal。禁止在 Activation CAS 失败后立即删除对象。
- **Depends on / blocked by**：不可变 `PortalActivation` 历史模型、Origin 对象索引、恢复版本保留策略、多 Edge 使用状态和运维容量基线。

## Mobile/Runner 授权载体与执行租约

- **What**：在 Mobile 和 Runner 内核推进时完成 B7：各端可信身份载体、Runner Execution Lease、离线授权上限、撤权与租约失效闭环。
- **Why**：Portal 的签名 Permission Catalog、Policy、Enforcer、在线角色与主体绑定已经完成，但移动端和桌面执行器不能照搬浏览器 Cookie，也不能让离线任务无限继承人的在线权限。
- **Pros**：四类内核共享同一授权语义；Runner 获得最小、短时、可审计的任务执行权限；设备丢失、主体撤权或策略换代后可以确定失效。
- **Cons**：需要设备身份、租约签发、续期、离线窗口、任务绑定、重放防护和时钟偏差测试，与 Runner/Mobile 生命周期紧密相关，不能脱离对应内核单独完成。
- **Context**：B1—B6 已完成；`platform.admin/is_admin` 通用旁路及 legacy operation-role 表已经移除。B7 不新增第二套角色系统，只为不同终端投影合适的可信载体。
- **Depends on / blocked by**：Runner Profile 的构建、签名与实际装配，Mobile Profile/Gateway/Native Adapter，以及设备注册和吊销模型。权威设计见《[在线角色与权限治理](docs/dev/architecture/在线角色与权限治理.md)》与 [ADR-0106](docs/dev/decisions/ADR-0106-多端统一身份授权与Runner执行租约.md)。

## Portal 用户偏好服务端真源

- **What**：把当前浏览器本地的 Shell Library、Renderer、主题和 Workbench 展示偏好提升为租户/Portal/用户隔离的服务端 `PortalPreference`，localStorage 只保留启动缓存。
- **Why**：当前用户已经可以即时切换 Shell Library，且只下载所选模块，但偏好只保存在当前浏览器配置文件，无法跨设备同步，也不能在管理员撤销允许项后形成集中失效事实。
- **Pros**：跨设备一致；管理员调整允许范围后可确定回退；偏好 revision 可参与候选切换和审计，而不需要修改 Platform Profile 或创建 Activation。
- **Cons**：需要认证后的窄 BFF、用户级 CAS、隐私边界、离线缓存和失败回退；不能让偏好接口接受模块 URL、版本、摘要、CSS 或任意组件配置。
- **Context**：Shell Catalog、deferred Library、允许范围校验和候选 Generation 已实施。服务端只保存稳定选择 ID；浏览器仍以活动 RuntimeSpec 的允许目录为权威，候选成功后才提交偏好。
- **Depends on / blocked by**：稳定企业主体、Portal Session、偏好 Store 与多 Node 一致性。

## 生产 Portal Activation 实时模式容量验收

- **What**：在生产启用 `updates.mode=notify|automatic` 前，对已实现的认证 SSE 更新链路完成容量、代理兼容、断线恢复和集中更新控制验收。
- **Why**：Portal 已支持 `refresh|notify|automatic`，但生产默认仍是 `refresh`。实时模式会引入常驻连接、多 Node 分发和激活瞬间的候选装配峰值，不能仅凭功能测试默认开启。
- **Pros**：管理员激活后，长期打开的页面可及时提示或事务切换 Generation；候选失败仍保留活动 Generation。
- **Cons**：需要目标负载基线、代理超时配置、认证续期、退避重连、重复事件、多 Node 传播和刷新风暴控制。
- **Context**：Node Portal Kernel 已提供认证、租户/Portal 隔离的 SSE，只分发最小 revision 事实；浏览器仍重新获取权威 RuntimeSpec，不信任事件携带模块内容。生产在验收完成前继续使用低负载的 `refresh` 默认值，禁止页面轮询仓库。
- **Depends on / blocked by**：真实生产并发基线、多 Node 部署、RuntimeSpec/PortalGeneration 候选切换和目标代理环境。
