# VastPlan Database User Authentication Provider

这是可选企业身份 Provider，不是内核用户系统。它只通过稳定 `foundation.data.relational.runtime` 查询端口读取一条身份记录，并验证标准 Argon2id PHC 字符串；数据库连接、驱动和连接池继续由 Database Runtime 与连接管理插件拥有。

Provider 不绑定 PostgreSQL。每个 Profile 配置自己的只读单参数 `lookupSql`：PostgreSQL 可使用 `$1`，MySQL 可使用 `?`，未来其他 Runtime Provider 使用其驱动支持的占位符。查询禁止分号和 SQL 注释，只允许 `SELECT`，最多返回两行；零行或重复行均返回同形 `authentication.invalid_credentials`。

部署必须向本插件投影精确 `database.connection/<resourceId>` grant。未配置数据库、连接未激活或 Runtime 不健康时，本 Provider 保持 Blocked/不可用，但 Seed Access 与 OIDC Provider 继续工作。

密码列必须保存 Argon2id PHC 字符串，不允许明文、可逆密文或供应商专用弱摘要。Provider 只返回稳定 subject/issuer Evidence，不返回用户行、Group、角色或权限。
