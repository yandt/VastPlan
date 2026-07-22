# Authorization Session

可信 Portal BFF 在验证并消费 Authentication Broker Assertion 后，通过本插件把 `(providerProfileId, issuer, subject)` 转换为不可碰撞的内部主体，并从签名 Policy Snapshot 投影会话所需的无条件权限。

它不是用户系统，也不读取外部 Provider 的 Group/Role claim。带资源或属性约束的权限不会被扁平化进 Session，仍由 Authorization Engine 逐请求判断。

运行配置：

- `VASTPLAN_AUTHORIZATION_POLICY_SNAPSHOT`：已发布 `SignedPolicySnapshot` 的绝对路径；
- `VASTPLAN_AUTHORIZATION_POLICY_TRUST`：包含 `{version:1,keys:[{keyId,publicKey}]}` 的 Ed25519 公钥信任文件；
- Snapshot audience 必须包含 `portal:<tenantId>:<portalId>`；
- Policy Binding 的主体 ID 使用 `StableSubjectID(providerProfileId, issuer, subject)`，issuer 固定为 `vastplan.authentication`。

当前 File Snapshot Store 是 Seed/测试适配器；上层 `foundation.security.authorization-session` 协议保持不变，后续可替换为在线 Authorization Store/Engine 组合。
