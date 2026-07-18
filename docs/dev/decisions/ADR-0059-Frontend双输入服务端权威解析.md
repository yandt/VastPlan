# ADR-0059 Frontend 双输入采用服务端权威解析

- 状态：已采纳
- 日期：2026-07-18
- 关联：[ADR-0052 前端门户内核与多 UI 设计系统插件](ADR-0052-前端门户内核与多UI设计系统插件.md)、[ADR-0057 插件分级管理与双输入组合解析](ADR-0057-插件分级管理与双输入组合解析.md)、[ADR-0058 跨内核组合公共契约与内核适配器](ADR-0058-跨内核组合公共契约与适配器.md)

## 背景

Frontend 内核运行在浏览器，但 Platform Profile 的设计系统、安全基线和插件来源属于平台授权边界。若在浏览器合并双输入，应用代码可伪造来源或绕过制品分类；若让 Portal Composer 自行判定制品身份，平台插件又会获得不应持有的仓库凭据和验签密钥。

## 决策

1. `schemas/composition/frontend/v1` 定义 Frontend Platform Profile 与 Application Composition；两者复用公共文档身份、`target.kernel`、来源和摘要语义。
2. Portal Composer 接收的普通 Draft 只包含 Application Composition。环境绑定的 Platform Profile 由内核配置回调注入，浏览器请求不能携带或覆盖。
3. Composer 执行确定性合并并生成带两份输入 `id/revision/digest`、Portal revision 和逐插件 origin 的锁定 `PortalSpec`。
4. 可信 Portal Catalog 在内核边界读取并验证精确制品，复核 publisher/命名空间分类、Frontend engine、单一设计系统、完整 UI contract 与 origin；Composer 不取得制品内容或信任根。
5. Portal Runtime 只消费锁定 `PortalSpec`，再次检查输入锁、插件来源、设计系统来源、签名 provenance 和 UI contract，不解析原始双输入。
6. Platform Profile 在一个 Composer 进程生命周期内不可切换。平台升级通过候选 Portal Edge/Composer 实例预检后切换，避免同一进程内历史 revision 使用不确定基线。

## 备选方案

- **浏览器内解析**：无法形成可信来源和平台权限边界，拒绝。
- **Composer 直接读取仓库和验签密钥**：扩大基础插件权限并复制内核信任逻辑，拒绝。
- **把设计系统继续放在应用草稿**：应用可替换平台基线，与 ADR-0057 冲突，拒绝。

## 影响

- 正面：普通 Portal 编辑者只能选择应用功能插件；设计系统和平台插件独立治理。
- 正面：浏览器、Composer 和内核 Catalog 各自只承担一层校验，形成纵深防御。
- 代价：Portal Edge 启动必须显式绑定 Frontend Platform Profile，平台升级需要候选实例切换。
- 约束：发布和回滚都必须针对当前进程绑定的平台基线重新解析，不能直接复用历史锁定结果。
