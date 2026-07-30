# ADR-0170 统一 Capability Contract 与可信 ArtifactIdentity

- 状态：已采纳，分阶段实施
- 日期：2026-07-30

## 背景

同一个插件操作曾同时维护在 Manifest、Go 运行时 descriptor、Portal Host 路由白名单和权限策略中。增加 `deleteProfileDraft` 时，业务实现与前端已经存在，但旧 Host 未声明该路由，最终返回 405。插件版本又分别出现在 Manifest、语言包、运行常量和部署锁中，容易形成稳定制品身份漂移。

问题不是单个路由缺失，而是“能力、操作、受众、权限和制品身份”存在多个可编辑真相源。继续逐点同步会让每次升级都依赖人工记忆。

## 决策

1. 签名 `vastplan.plugin.json` 是插件静态能力与版本的唯一真相源。Backend Tool 经 `ManifestToolCapabilityContracts` 归一化为 `ToolCapabilityContract`，包含插件身份、capability、operation、受众、参数/结果 Schema 和 operation guard。
2. Tool 可按工具逐步启用受众闭包。一旦任一 operation 声明 `audience`，同一 Tool 全部 operation 都必须分类为 `user/workload/system`；用户操作必须且只能由 Manifest 的 `authorization.operationGuards` 绑定权限，非用户操作不得伪装用户权限守卫。
3. 权限 namespace 与 capability 名不同的首方插件，必须通过 `authorization.capabilities` 显式声明所有权关系；不能靠字符串相似度或 Host 特例放行。
4. 运行时 descriptor、插件处理器注册集合、Portal Host 可调用操作联合类型和运行时白名单均由归一化契约生成。发布 `plan/prepare` 把缺失或漂移的投影列为同一发布计划的派生改动。
5. 需要按状态动作区分权限的接口必须拆成稳定 operation，例如 `submitPortalVersion/approvePortalVersion/publishPortalVersion`，不得在 Enforcer 之后再从 payload 猜测权限。
6. 用户授权只在 Kernel Enforcer 执行一次。业务插件继续校验可信主体、租户、资源所有权、对象状态、CAS 和职责分离，但不得再以 Token 中的旧角色名重复授权，否则在线 Role/Binding 会失效。workload/system 仍由窄 Policy Bundle 和 Kernel Service Grant 处理。
7. 插件 Manifest SemVer 同步投影到选中插件的语言包版本和部署精确引用；这些文件不再是可独立编辑的版本源。
8. 可信宿主继续使用现有 `runtimeidentity.Identity` 绑定 `pluginId/publisher/version/artifactSha256/node/runtime/instance`。身份来自已验证制品和 LaunchPolicy，绝不接受插件自报。后续 Node Worker、Python 子解释器和独立进程 SDK 只消费宿主注入的只读投影；调用和审计以宿主身份为准。

## 分阶段实施

- C1：Portal Composer 迁移为统一契约，生成 Go 与 TypeScript 投影，用户操作进入 Permission Catalog；Authorization Enforcer 删除 Portal 用户角色表。
- C2：发布器为所有选中插件统一同步语言包版本，并在启动/发布前检查投影收敛。
- C3：将生成器推广到其他受治理 Tool，并给 Node/Python 共享 Runtime 注入逐 Worker/逐解释器的可信 ArtifactIdentity 只读视图。
- C4：开发热替换按改动类型分类；仅业务模块变更替换插件 generation，Host 契约或路由投影变更自动重启 Host generation，不让旧 Host 静默继续服务。

## 备选方案

- 继续手工维护各层 operation 列表：改动小，但已经证明容易漏同步，不采用。
- 允许 Portal Host 接受任意 operation 并转发：可以避免 405，却扩大代理能力和攻击面，不采用。
- 把所有接口改为动态注册路由：会把插件 ID、路由冲突和授权顺序重新带回 Host，不采用。
- 只根据 payload 动作做授权：Enforcer 看不到稳定 operation，审计与策略缓存不可靠，不采用。

## 影响

新增用户操作必须先改 Manifest，生成投影后编译器和预检会同时暴露遗漏。操作名称会更明确，数量略增；发布工具也多了一步派生同步，但人工同步点显著减少。旧 Tool 可以逐个迁移，不要求一次改完所有插件。运行期仍使用精确制品摘要，不因契约兼容而放松稳定制品不可覆盖规则。
