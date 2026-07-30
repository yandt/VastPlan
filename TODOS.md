# VastPlan 后续任务

> 本文件只记录已经明确推迟、且具备充分上下文的跨阶段任务。当前实施范围与架构单一真相源仍以 [`docs/dev/00-index.md`](docs/dev/00-index.md) 为入口。

## Portal 制品快照可达性回收

- **What**：为 Portal 中央交付 Origin 和 Edge cache 建立基于 `PortalActivation` 历史的内容寻址对象可达性回收。
- **Why**：候选物化后可能因治理 CAS 冲突而未激活；历史 Activation 过期后也会留下不再引用的模块和快照。长期只保留会持续占用磁盘。
- **Pros**：自动控制存储容量；保留仍需恢复的历史；避免人工误删被多个 Portal 共用的 digest。
- **Cons**：必须处理共享对象、保留宽限期、多 Edge 读者、删除墓碑、崩溃恢复和并发激活，属于独立的制品生命周期工程。
- **Context**：当前阶段只记录 orphan 的 digest、时间和失败原因，并设置容量与对象数告警；达到硬上限时拒绝新物化但不影响活动 Portal。禁止在 PortalRelease CAS 失败后立即删除对象。
- **Depends on / blocked by**：不可变 `PortalRelease` 历史模型、Origin 对象索引、恢复版本保留策略、多 Edge 使用状态和运维容量基线。

## Mobile/Runner 授权载体与执行租约

- **What**：在 Mobile 和 Runner 内核推进时完成 B7：各端可信身份载体、Runner Execution Lease、离线授权上限、撤权与租约失效闭环。
- **Why**：Portal 的签名 Permission Catalog、Policy、Enforcer、在线角色与主体绑定已经完成，但移动端和桌面执行器不能照搬浏览器 Cookie，也不能让离线任务无限继承人的在线权限。
- **Pros**：四类内核共享同一授权语义；Runner 获得最小、短时、可审计的任务执行权限；设备丢失、主体撤权或策略换代后可以确定失效。
- **Cons**：需要设备身份、租约签发、续期、离线窗口、任务绑定、重放防护和时钟偏差测试，与 Runner/Mobile 生命周期紧密相关，不能脱离对应内核单独完成。
- **Context**：B1—B6 已完成；`platform.admin/is_admin` 通用旁路及 legacy operation-role 表已经移除。B7 不新增第二套角色系统，只为不同终端投影合适的可信载体。
- **Depends on / blocked by**：Runner Profile 的构建、签名与实际装配，Mobile Profile/Gateway/Native Adapter，以及设备注册和吊销模型。权威设计见《[在线角色与权限治理](docs/dev/architecture/在线角色与权限治理.md)》与 [ADR-0106](docs/dev/decisions/ADR-0106-多端统一身份授权与Runner执行租约.md)。

## 生产 Portal Release 实时模式容量验收

- **What**：在生产启用 `updates.mode=notify|automatic` 前，对已实现的认证 SSE 更新链路完成容量、代理兼容、断线恢复和集中更新控制验收。
- **Why**：Portal 已支持 `refresh|notify|automatic`，但生产默认仍是 `refresh`。实时模式会引入常驻连接、多 Node 分发和激活瞬间的候选装配峰值，不能仅凭功能测试默认开启。
- **Pros**：管理员激活后，长期打开的页面可及时提示或事务切换 Generation；候选失败仍保留活动 Generation。
- **Cons**：需要目标负载基线、代理超时配置、认证续期、退避重连、重复事件、多 Node 传播和刷新风暴控制。
- **Context**：Node Portal Kernel 已提供认证、租户/Portal 隔离的 SSE，只分发最小 Release revision 事实；浏览器仍重新获取权威 RuntimeSpec，不信任事件携带模块内容。生产在验收完成前继续使用低负载的 `refresh` 默认值，禁止页面轮询仓库。
- **Depends on / blocked by**：真实生产并发基线、多 Node 部署、RuntimeSpec/PortalGeneration 候选切换和目标代理环境。

## 插件兼容升级候选差异与人工确认

- **What**：管理中心基于原 Application Intent 与新的 Catalog revision 重新规划精确 `ArtifactLock`，展示插件新增、删除、升级、降级、渠道变化和 digest 变化，并由具备发布权限的管理员确认后形成新的候选 Revision。
- **Why**：Backend P0/P1 只建立“范围输入、精确锁定、显式重新规划”的内核闭环；如果没有可读的锁差异，管理员仍可能在不了解传递依赖变化的情况下盲目刷新和发布。
- **Pros**：升级过程可审计、可解释；active revision 不会静默漂移；审批者能够在发布前识别跨兼容边界、渠道变化和制品内容变化。
- **Cons**：需要管理 API、在线角色权限、候选状态持久化和前端 Workbench 协同；还要处理 Catalog revision 变化、重复刷新、并发审批和旧候选失效。
- **Context**：应用根配置只允许精确 `=x.y.z` 或兼容 `^x.y.z`；Manifest 传递依赖仍可使用完整 SemVer。当前阶段新版本进入仓库只会让待审批方案按既有 stale 规则失效，不会改变已发布服务。后续 UI 应展示 Intent digest、旧/新 Catalog revision 和旧/新 Lock digest，禁止直接修改 active Lock。权威边界见 [ADR-0158](docs/dev/decisions/ADR-0158-应用插件兼容范围与精确运行锁.md)。
- **Depends on / blocked by**：Backend Application Intent 范围输入、Feature 感知 Resolver、精确 `ArtifactLock` 和显式 refresh/publish 流程稳定；实施时补充管理中心候选差异与人工确认设计。

## Portal、Runner、Mobile 应用插件范围输入

- **What**：在 Portal、Runner、Mobile 的应用插件选择输入层复用 `plugin/v1.ArtifactRequirement` 和统一错误模型，规划后仍生成各内核可消费的精确 Lock/Profile；不把范围扩散到平台基础和恢复配置。
- **Why**：Backend 先行落地后，如果其他内核各自设计版本字段、兼容规则和刷新语义，会形成四套不一致的依赖管理机制。
- **Pros**：四类内核共享 exact/caret 输入语义、Feature 可选依赖、候选回溯、渠道冲突和精确锁边界；插件开发者与平台管理员只需理解一套规则。
- **Cons**：三个内核的应用输入边界不同，必须分别梳理 Portal 应用插件、Runner 工作流插件和 Mobile 能力插件，不能机械替换所有 `ArtifactRef`；需要较大的跨模块回归测试。
- **Context**：仅应用插件选择允许范围。Portal Platform Profile、Frontend Runtime Engine、Render Adapter、Shell、Workbench、Seed/LKG、Runner/Mobile 已编译 Profile 和所有 Activation/Deployment/Assignment/回滚记录继续使用精确版本与 digest。
- **Depends on / blocked by**：Backend P0/P1 和共享 Requirement Schema 稳定；Portal、Runner、Mobile 各自的在线应用组合输入与发布边界明确。实施前需逐内核制定小范围 ADR，禁止一次性替换全部精确引用。
