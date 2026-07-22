# VastPlan Seed Access

本插件只提供首次安装和灾难恢复所需的最小认证能力，不是普通用户系统。企业日常登录必须交接给 OIDC、SAML、LDAP、数据库用户等可选择的 `authentication.provider` 插件。

核心边界与状态机见《[企业身份与种子访问](../../../docs/dev/architecture/企业身份与种子访问.md)》和 [ADR-0109](../../../docs/dev/decisions/ADR-0109-种子访问与企业身份Provider分离.md)。

## 安全与运行约束

- 推荐语言为 Go：状态机、Argon2id、CAS 和文件系统持久化均可复用现有 Backend 工程能力；Python/Node 在此处没有明显生态优势，反而扩大依赖与供应链面。
- 运行形态为第一方可信独立进程，使用 `leader + leader-owned + cluster + leader`；调度范围只包含受信 Seed Host，不在所有业务 Backend 副本重复启用。
- `VASTPLAN_SEED_ACCESS_STATE_FILE` 必须是受保护目录中的绝对 `.json` 路径；目录不可被 group/other 写入。
- `VASTPLAN_AUTHENTICATION_ASSERTION_TRUST` 指向 Broker Ed25519 公钥信任文件；`VASTPLAN_AUTHORIZATION_POLICY_SNAPSHOT` 与 `VASTPLAN_AUTHORIZATION_POLICY_TRUST` 指向已签名授权快照及其信任文件。
- 状态文件只保存 Argon2id verifier、精确 Provider/Policy 引用和交接/恢复摘要，不保存明文密码、企业用户目录、Token、角色或 Client Secret。
- 企业交接后 `seed-password` 自动不可用。恢复必须先由本机运维证明打开最长 15 分钟的 Recovery Lease。

## 当前实现

- Seed 状态机、Provider 配置/真实身份验证/交接 Seal、企业启用和恢复租约；
- 0600、无符号链接、跨进程锁、CAS generation、临时写入、fsync 与原子 rename 的 File Store；
- Argon2id Seed Operator verifier；
- 与所有企业 Provider 相同的 `authentication.method.v1`，固定语义表单且不提供任意前端代码。
- `foundation.security.seed.handoff` 管理能力：只接受可信 Portal 调用，使用 Broker 签名 Assertion 验证企业主体，并确认同一稳定主体已进入有效签名 Policy Snapshot 后才允许 CAS 交接。

本机初始化和恢复使用无网络监听的 `engineering/tools/seedaccessctl`。密码与恢复证明只从 owner-only 普通文件读取，不能放入命令行参数；管理 Workbench 在后续控制面阶段接入。生产不得以直接编辑 JSON 替代这些入口。
