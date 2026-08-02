# 插件市场

`cn.vastplan.platform.artifacts.marketplace` 是平台级多来源插件市场。它通过受治理 Profile 管理多个可自定义 URL，向 Portal 提供来源目录和有界查询；URL、凭证引用和 TLS 规则不会由浏览器请求决定。

## 边界

- Marketplace 负责“发现”：列出来源、查询目录和标识哪些条目可由服务选择。
- Artifact Repository 负责“信任与保存”：远端制品必须按精确引用导入并重新验签后才能进入本地 Catalog。
- Deployment Manager 负责“变更”：安装、升级、卸载继续进入统一候选、审批、Generation 与回滚链。
- Marketplace 不直接加载插件、不写 Application Intent，也不保存凭证明文。

每个来源配置 `id`、`label`、`url` 和 `priority`。URL 可包含规范路径前缀，但不能包含凭证、查询参数、fragment 或路径穿越，运行时也不会跟随 HTTP 重定向。生产 URL 强制 HTTPS；开发环境只允许显式开启的 loopback HTTP。需要认证时配置归属于本插件、用途为 `artifact.marketplace.read-token` 的托管凭证引用，运行时通过 material lease 临时读取。每个来源持有自己的不透明凭证 handle，明文不会进入启动配置。

当前平台自己的受信 Catalog 可使用 `vastplan://platform.artifacts.repository`，该地址经 capability 路由而不是 HTTP 网络访问，适合作为 Seed 默认市场；其他企业或合作伙伴市场继续使用自定义 HTTPS URL。

外部来源当前用于目录发现；其条目必须先经过受治理的导入、验签和镜像流程进入 Artifact Repository，Portal 才会开放安装动作。这样自定义市场 URL 不会绕过本地信任根和稳定制品身份规则。

```json
{
  "sources": [
    { "id": "enterprise", "label": "企业插件市场", "url": "https://plugins.example.com", "priority": 10 },
    { "id": "partner", "label": "合作伙伴市场", "url": "https://partner.example.com", "priority": 20 }
  ]
}
```
