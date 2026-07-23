# @vastplan/credential-reference

`ManagedCredentialRef` 的 Node.js 基础 SDK。它只验证和规范化非敏感引用，不提供 material 读取或解密能力。

配置控制器、资源控制器和 Material Lease 客户端必须复用这里的闭合字段、scope、owner、purpose 和版本校验，不能各自维护同义结构。
