# ADR-0112 PortalPreference 服务端真源与候选提交事务

- 状态：已采纳
- 日期：2026-07-23

## 背景

Portal 已允许用户切换 Renderer、Shell Library、主题、图标风格和 Workbench 集合展示，但早期选择只保存在浏览器。它不能跨设备同步，也无法在管理员撤销允许项后形成集中、可审计的失效事实。若把用户选择写回 Platform Profile 或 Portal Activation，则每次列显隐、分页大小都会制造平台发布，混淆管理员治理事实和个人体验状态。

## 决策

1. 建立独立能力 `platform.portal-preference`。它与 Portal Composer 共用 leader-owned 服务进程和集群寻址，但使用独立状态文件、请求模型、权限和审计；偏好故障不能改写 Profile/Application/Binding/Activation。
2. 记录按认证后的 `tenant + subject + portal + Renderer/Shell/Workbench catalog ID 与 contract major` 隔离。tenant、subject 和 scope 均由可信 Node Portal Kernel 从会话及当前活动 RuntimeSpec 投影，浏览器请求不能提交或覆盖这些字段。
3. 服务端记录是跨设备真源；经过同一 scope 校验的 localStorage 只作启动缓存。解析顺序固定为服务端有效记录、验证过的缓存、Platform Profile 默认。服务端 revision 为零时可迁移缓存；服务端不可用时允许使用缓存启动，但不把失败写入伪装成成功。
4. 服务只保存稳定选择 ID：Renderer ID、每 Renderer 的主题/图标 ID、Shell Library ID，以及每个 Collection 的列顺序、隐藏列、密度和分页大小。禁止 URL、版本、channel、摘要、CSS、DOM、React 节点、任意配置和权限结论。
5. 写入采用完整 values 的 revision CAS。相同旧值重放幂等；真正冲突返回稳定错误。Workbench 通过窄 `WorkbenchPreferencePort` 读写单个 Collection，Portal Kernel 在冲突后重新读取并合并一次，Workbench 不接触身份、CSRF、HTTP 或完整偏好文档。
6. Shell Library、主题和图标先准备候选 Portal Generation，成功后才提交偏好；失败回滚活动 Generation。Renderer 切换先写本机 `pending` Host Epoch，刷新并成功启动候选 Renderer 后才提交服务端，失败清除 pending 并恢复当前安全值。
7. Profile 的允许目录始终高于用户偏好。被撤销、未知或契约 scope 已变化的 ID 不参与装配，运行时确定回退 Profile 默认；旧记录不扩大模块下载范围、字段集合或权限。
8. 只有 `portal.bff` 用户场景且具有有效 subject 的调用可以 get/put。普通插件、匿名浏览器、SYSTEM 调用和跨租户请求均拒绝；状态文件要求私有真实目录、`0600` 原子替换、大小与记录数上限。

## 备选方案

- **继续只用 localStorage**：实现最简单，但不能跨设备同步、集中撤销或审计。
- **写入 Platform Profile**：具备中心存储，却把个人体验变成管理员发布并产生大量 Activation，否决。
- **每种偏好建立一个 API**：局部写入简单，但身份、CAS、离线和审计逻辑重复，且难以原子处理 Renderer 及其主题选择。
- **单独增加偏好常驻服务进程**：隔离最强，但当前数据量和扩缩容需求不足以抵消额外进程、调度和运维成本。保留独立 capability/state 后，未来仍可按契约拆分。
- **先保存选择再尝试装配**：可能形成刷新循环或跨设备传播不可启动选择，否决。

## 影响

- 用户的布局、Renderer、主题和集合展示可跨设备同步；本机断网时仍能用已验证缓存启动。
- Portal Preference 不改变插件制品锁、Platform Profile、Activation 或 Backend 服务组合。
- 完整文档 CAS 需要客户端在冲突后合并；当前 Collection 写端已串行并只重试一次，持续争用会明确提示用户而不是静默覆盖。
- 当前文件 Store 是 leader-owned 开发/单卷适配器。生产多节点必须给该逻辑服务提供一致的持久卷或替换为实现同一 CAS 契约的共享 Store，不能让多个 leader 分别持有真源。

## 后续实施

- 2026-07-23：Portal Composer 1.6.0 已按 [ADR-0125](ADR-0125-Portal-Composer与Preference共享状态分区.md) 将偏好迁移为 `tenant + subject` Shared State 文档，并将组合治理迁移为 tenant CAS 聚合；插件切换为 active-active。上文独立状态文件描述保留为本 ADR 采纳时的历史记录。
