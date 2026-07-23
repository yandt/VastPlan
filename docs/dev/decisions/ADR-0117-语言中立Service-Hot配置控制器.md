# ADR-0117 语言中立 Service Hot 配置控制器

- 状态：已采纳，Go 首个实现已完成
- 日期：2026-07-23
- 关联：[ADR-0047](ADR-0047-多语言运行驱动与第三方隔离边界.md)、[ADR-0113](ADR-0113-可信插件配置目录与分路径生效.md)、[ADR-0114](ADR-0114-一次性ConfigurationAuthority与委托凭证暂存.md)

## 背景

`service + hot` 的 Active 配置属于目标插件进程，不能由内核、global-settings 或 plugin-settings 保存为第二真相源。目标插件可能运行在 Go、Node、Python 或其他 Runtime 中，也可能处于独立进程、语言共享 Runtime 或受控首方内嵌形态，因此热配置不能依赖 Go 接口、共享内存或某一种进程模型。

仅声明 `applyMode=hot` 也不等于具备安全热更新能力。协调器必须能判断目标是否真的实现配置控制器，精确路由到当前服务实例，并在进程中断、超时或响应丢失后查询事实，而不是盲目重放副作用。

## 决策

### 1. 增加语言中立的 `configuration.v1`

`contracts/schemas/configuration/v1` 定义 JSON wire 和闭合 JSON Schema，固定四个操作：

- `prepare`：校验 Active CAS、候选身份、非敏感 values 和托管凭证引用，耐久准备但不改变 Active；
- `commit`：按 candidate/request digest 幂等地原子切换 Active；
- `abort`：终止尚未提交的候选，不改变 Active；
- `status`：返回当前 Active 和可选候选的事实，用于超时与重启恢复。

响应只包含 revision、digest、状态、readiness 和稳定错误，不返回配置值、凭证 handle、密文或 material。插件使用自身最合适的语言实现状态机；Go SDK 只是一份适配器，不是协议真源。Node、Python 与未来 Runtime 必须实现同一 wire，不另造语言专用协议。

### 2. 控制器是由签名配置契约派生的专用贡献

插件在顶层 `configuration` 中同时声明：

```json
{
  "scope": "service",
  "applyMode": "hot",
  "controller": { "protocol": "configuration.v1" }
}
```

可信清单解析器由签名插件 ID 派生 `configuration.<sha256-prefix>` 不透明 capability，并合成 `configuration.controller` contribution。发布者不能自定义 capability，浏览器也不会获得真实目标、logical service 或 routing domain。Catalog 只在运行策略能形成唯一服务所有者或外部共享/队列一致性时生成 Controller Target；旧 hot 清单若没有 controller 声明，继续可安装但在线配置只读。

`configuration.controller` 使用 single 分发。它不复用 `tool.package`，避免把内部事务端口混入产品 API 和权限目录。运行时 Declaration 必须与清单合成结果完全一致，进程不能临时扩大控制能力。

### 3. plugin-settings 持有可恢复协调 Saga，但不持有 Active

`submitHotServiceDraft` 固定执行：重新读取活动 Catalog、校验制品/Schema/Controller Target、耐久写入 `Preparing`、委托暂存凭证、查询目标 Active、调用 `prepare`，再进入 `PendingApproval`。审批必须由不同主体完成。

`activateHotServiceCandidate` 固定执行：先用 `status` 复核已准备候选，再把托管凭证推进为 Active，最后调用控制器 `commit`。中断后从 `Activating` 检查点继续，以目标 `status` 判定提交是否已发生。提交前可进入 `Aborting`，依次调用控制器 `abort` 和凭证 abort。公开候选不包含 Controller Target、request digest、stage ID 或 credential handle。

控制器故障不会使旧 Active 失效：未提交候选保持可恢复，协调器不得把它显示为 Ready，也不得回退为直接写插件文件或重启进程。

### 4. 首个实现选择 Go OTP，但协议不绑定 Go

首个真实控制器放在 OTP 插件中。选择 Go 是因为该插件已有 Go 原生状态机、并发挑战 Store 与独立进程实现；在原进程内增加控制器不增加后台进程，也避免跨语言复制在途挑战 generation 固定逻辑。提交以 `0600` 原子文件、目录同步和 Active CAS 持久化；旧挑战继续使用创建时固定的 Profile，新挑战使用新 generation。

这不是后续插件的默认语言。每个插件仍须按驱动生态、资源占用、并发、隔离与维护成本独立比较 Go、Node、Python 及其他可用语言；运行形态也单独决策。

## 备选方案

- **由内核保存通用热配置 Store**：会把插件业务配置和迁移语义推入内核，并与目标进程形成双真相源，否决。
- **复用 `tool.package`**：会把内部事务操作暴露为普通业务工具并扩大 BFF/权限维护面，否决。
- **让 Manifest 自定义控制器 capability**：产生碰撞、伪装和浏览器路由泄露风险，否决。
- **每种语言定义自己的热配置接口**：恢复语义和摘要算法会分叉，跨 Runtime 无法统一治理，否决。
- **prepare 后立即 commit**：绕过异人审批，也无法安全协调候选凭证，否决。

## 影响

- Backend 公开插件扩展点由七个增加为八个，兼容矩阵和 descriptor Schema 固定 `configuration.controller`。
- hot-service 已形成可管理、可审批、可恢复路径。本 ADR 当时未实现的 `tenant/user + hot` resolve/watch 真源，已于 2026-07-23 由 [ADR-0119](ADR-0119-Tenant与User-Scoped-Hot配置真源.md) 独立落地；两条路径继续保持不同的 Active 所有权，不合并为双真相源。
- 0.8.0 首批只向无托管凭证字段的 Service Hot 定义开放操作；带秘密配置必须先补齐“保留旧引用 + 替换新引用”的完整摘要和恢复语义，当前 fail-closed 且界面显示不可用。
- 控制器作者必须持久化 Active 与候选事实并实现幂等；不满足者只能使用 restart 生效或保持只读。
- 第一方 Go 插件可复用 Go SDK；其他 Runtime 只需遵守 JSON wire，不需依赖 Go 构建或与内核整体编译。

## 实施记录（2026-07-23）

- `configuration.v1` JSON Schema、Go 类型与适配 SDK、`configuration.controller` descriptor/Registry、签名清单合成贡献和 Catalog 内部目标均已落地；兼容矩阵固定新扩展点。
- plugin-settings 0.8.0 已接入 Hot Draft Active 基线、异人审批、prepare/commit/abort/status 恢复、公开目标裁剪、最近 Ready 非敏感值投影和独立权限。草稿创建后的 Active 漂移会按 revision/digest fail-closed，私有基线可跨重启恢复。
- OTP 0.2.0 已实现首个真实 Go 控制器；platform-admin-access-policy 0.24.0 只授权精确 plugin-settings 调用四个标准操作。Go 全仓、前端全工作区、相关竞态测试全部通过。
- 本地 fresh 真实启动验收完成：Backend revision 21 的 11 个单元与受管服务 revision 2 的 1 个单元均收敛 Ready，plugin-settings 0.8.0 和访问策略 0.24.0 实际激活，Portal `/operations` 与 `/` 均返回 HTTP 200；随后已优雅停止。按既定决定未执行 soak。
- Node `@vastplan/configuration-controller-node` SDK 随后完成：使用现有共享 node-worker，不增加进程；调用者校验、闭合 wire、Observation 裁剪、capability 与 prepare/configuration digest 均由 Go/Node 双边 golden 锁定。OIDC/Webhook 的动态 profile 秘密仍待专门契约选择，未被伪装成已支持。

## 后续修正（2026-07-23）

本 ADR 第 3 节的“先把托管凭证推进为 Active，再调用 commit”已由 [ADR-0120](ADR-0120-Service-Hot托管凭证提交与退役.md) 修正。Candidate 凭证本来就允许受控 Material Lease；因此当前顺序为控制器原子 commit、耐久记录 `FinalizingCredentials`、再把 replacement 推进为 Active。这样 commit 失败时引用仍可 abort，commit 已成事实时又能从 status 恢复，不产生无人引用的 Active 凭证。其余 `configuration.v1` 四操作、Active 所有权和无 handle Observation 决策保持不变。
