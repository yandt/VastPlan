# VastPlan 后续任务

> 本文件只记录已经明确推迟、且具备充分上下文的跨阶段任务。当前实施范围与架构单一真相源仍以 [`docs/dev/00-index.md`](docs/dev/00-index.md) 为入口。

## Portal 制品快照可达性回收

- **What**：为 Portal 中央交付 Origin 和 Edge cache 建立基于 `PortalActivation` 历史的内容寻址对象可达性回收。
- **Why**：候选物化后可能因治理 CAS 冲突而未激活；历史 Activation 过期后也会留下不再引用的模块和快照。长期只保留会持续占用磁盘。
- **Pros**：自动控制存储容量；保留仍需恢复的历史；避免人工误删被多个 Portal 共用的 digest。
- **Cons**：必须处理共享对象、保留宽限期、多 Edge 读者、删除墓碑、崩溃恢复和并发激活，属于独立的制品生命周期工程。
- **Context**：当前阶段只记录 orphan 的 digest、时间和失败原因，并设置容量与对象数告警；达到硬上限时拒绝新物化但不影响活动 Portal。禁止在 Activation CAS 失败后立即删除对象。
- **Depends on / blocked by**：不可变 `PortalActivation` 历史模型、Origin 对象索引、恢复版本保留策略、多 Edge 使用状态和运维容量基线。

## 在线角色与权限配置基础插件

- **What**：新增平台基础插件，聚合已验证插件 Manifest 声明的权限目录，在线管理角色 revision、授权范围与发布审批。
- **Why**：插件可声明权限、内核可执行权限，但缺少面向管理员的角色组合、人员/主体绑定、即时撤权和审计闭环。
- **Pros**：安装插件后权限自动进入目录；角色无需手工维护中央权限代码；可以表达 platform、tenant、portal 和 resource 作用域。
- **Cons**：需要职责分离、角色版本、授权审批、主体目录对接、缓存失效、紧急撤权和失效权限清理，不能退化成简单复选框页面。
- **Context**：权限代码由所属插件在签名 Manifest 中声明，可信内核校验命名空间、scope、风险等级和冲突；角色插件只能授权 Catalog 中已知权限，不能创建权限，也不负责最终判定。
- **Depends on / blocked by**：插件权限 Manifest Schema、可信权限 Catalog、企业身份主体接口、内核授权执行点和本次 Portal Governance 分域权限。
- **Progress（2026-07-22）**：B1 已完成 Manifest 权限声明与确定性 Catalog；B2 已完成 Authorization IR、Policy Domain、Provider Profile、签名快照 wire shape 及 store/engine/directory/exchange Provider Protocol。Policy 状态机与签发、每内核 Enforcer、在线 Role/Subject Binding 和管理 Workbench 仍待 B3—B6。权威设计见《[在线角色与权限治理](docs/dev/architecture/在线角色与权限治理.md)》。

## 会话前登录与认证方法插件

- **What**：实现会话前 Access Generation、统一 AuthenticationFlow、Authentication Broker，以及首批密码和临时验证码 Method Provider。
- **Why**：当前生产 OIDC 会直接跳转外部 Provider，普通 Portal Generation 又依赖已认证 Principal，无法安全承载本地多方式登录页面。
- **Pros**：登录 UI 继续遵守 Runtime/Renderer/Shell/Workbench 层级；密码、OTP、OIDC、Passkey 可通过稳定协议扩展；Node 仍是唯一浏览器 Session 签发者。
- **Cons**：需要集群 transaction、一次性 Assertion、账号枚举防护、密码 pepper、验证码 Delivery、pre-auth CSRF 和独立公网安全验收。
- **Context**：Method Provider 不提供前端组件、不签发 Cookie、不返回角色；密码与验证码作为替代登录都只是单因素。权威设计见《[登录与认证协议](docs/dev/architecture/登录与认证协议.md)》。
- **Progress（2026-07-23）**：L0 已完成 Method/Assertion/Access Profile 公共 DTO、JSON Schema、解析与测试；L1—L6 的 Access Generation、UI、Broker、Password/OTP 和 OIDC 协议化仍待实施。

## 生产 Portal 实时 Activation 通知

- **What**：在出现明确实时需求后，为已打开的 Portal 增加生产 Activation revision 通知，使新布局或插件组合无需用户刷新即可事务替换 Generation。
- **Why**：当前策略只在刷新、新标签页或重新打开 Portal 时取得最新 Activation；长期打开的页面只会在正常 API 响应发现版本变化后提示刷新。
- **Pros**：管理员激活后，在线用户可以及时加载新 RuntimeSpec；候选失败仍能保留旧 Generation。
- **Cons**：需要常驻连接容量、代理支持、认证续期、断线重连、重复事件、多 Edge 分发和客户端集中刷新峰值控制。
- **Context**：当前不实现 SSE 或轮询。若未来实施，Edge 应复用一条 Activation KV watch 并向浏览器多路分发最小 revision 事件；浏览器必须重新获取权威 RuntimeSpec，不能信任事件携带模块内容。
- **Depends on / blocked by**：生产并发连接容量测试、Activation KV watch、多 Edge 传播、RuntimeSpec ETag 和 PortalGeneration 替换协议。
