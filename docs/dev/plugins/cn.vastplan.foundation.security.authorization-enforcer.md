# Authorization Enforcer

插件 ID：`cn.vastplan.foundation.security.authorization-enforcer`
当前制品版本：`0.4.4`

该 foundation 插件在每个 Backend unit 内以 per-kernel + local-ephemeral + direct 运行，位于用户调用的最终本地 PEP。它严格验证 Catalog、签名 Snapshot、audience 和有效期；未知目录操作弃权给 workload policy，目录内用户操作缺少策略或权限时拒绝。

平台管理、Portal、Interaction 与 Approval Provider 协议的首方无状态 workload 规则以四个独立 Policy Bundle 包编译进同一签名 Enforcer 制品，保留稳定 Checker ID、优先级和单元测试，但不再形成额外进程。Bundle 只承载策略内容；注册、调用、上下文裁剪和 fail-closed 强制仍走统一协议总线。

`0.4.3` 起，所有 workload Policy Bundle 必须先确认 capability 属于自身职责，再校验该能力所需的 caller、tenant 与 scene。无关能力必须直接弃权，禁止因为缺少本策略所需的上下文字段而越界拒绝；最终仍由匹配该能力的策略及零/全弃权 fail-closed 规则决定是否放行。

`0.4.4` 起，平台管理 Bundle 显式拥有 Platform Control SQL Bootstrap 与 Shared State SQL 宿主数据面的授权边界。它只允许固定 `platform-control-bootstrap/primary` SYSTEM caller 在 `platform.control.bootstrap` scene 下调用标准 `test/initialize/open` 与 `get/create/update/delete/list` 操作；其他身份、场景、扩展点和未知操作全部拒绝。

`0.3.9` 起，每个调用源内核只按稳定 `foundation.security.approval-policy` capability 放行保留可信 Principal 的插件调用，不钉死 Composer、未来消费者或具体 Provider 插件 ID；Provider 目标侧仍重复校验 caller、actor 与 tenant。源端和目标端复用 Approval Go SDK 的同一访问判定，避免双强制点漂移。

`0.3.10` 增加 Marketplace 最小能力边界：只允许精确首方 Marketplace 插件调用 Artifact Repository 的 `listCatalog`，并允许其向可信宿主申请绑定自身 CredentialRef 的 runtime material lease；其他仓库解析、下载或写入 operation 继续拒绝。

`0.3.7` 起，平台管理 Bundle 精确允许 `authorization-policy` 使用其 Manifest 已申请的 `get/fenced.create/fenced.update` Shared State 回调；普通非 fenced 写入、删除和未声明操作继续拒绝。该授权只解决插件到可信宿主的 workload 回调，不替代用户对角色与权限管理 API 的在线授权。

Portal Composer 用户操作从 `0.3.0` 起完全交给签名 Permission Catalog 与在线 Role/Binding；Portal Policy Bundle 对用户调用弃权，不再维护第二份 `portal.*` 角色表。Bundle 仅保留 system break-glass 和 Composer 访问 Kernel Service 的精确 workload 规则；最终资源状态仍由 Composer 强制，业务审批决定由 `approval.policy.v2` Provider 返回。

默认 `authorization.engine.v1` Native Provider 作为独立插件产出有界 Decision Proof；未来 Cedar、Casbin 或远端 PDP Adapter 可以实现相同协议，但不能绕过 Snapshot 验签和 Enforcer 的最终缓存上限。外部组只从可信 `authorization.directory.v1` 投影读取，再与 Published Group Binding 匹配。
