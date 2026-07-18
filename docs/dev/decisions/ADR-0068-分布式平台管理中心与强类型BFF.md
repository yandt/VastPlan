# ADR-0068 分布式平台管理中心与强类型 BFF

- 状态：已采纳
- 日期：2026-07-18

## 背景

全局设置、凭证、数据库连接和制品仓库已经是独立集群能力。Portal 需要统一管理入口，但不能把四个领域反向聚合进一个巨型插件，也不能向浏览器开放任意 capability/operation 代理。

## 决策

1. 管理页面归各领域插件所有，随其签名制品发布；Portal 只按 Frontend Platform Profile 动态组合页面和菜单。
2. Portal Edge 提供 `/v1/platform/*` 白名单强类型 BFF。租户、用户和角色只来自服务端会话；浏览器不能提交 target、capability、operation 或 tenant。
3. Edge 通过既有 `addressing.Router` 和 `routingDomain=platform` 调用远端 leader，保持平台能力位置透明；不在 Portal Edge 内重复启动 settings、credentials、database 或 repository。
4. 新增本地基础插件 `platform-admin-access-policy`，在能力所在 unit 作最终角色授权。Edge 的角色检查是前置纵深防御，不替代 Backend `permission.checker` 强制点。
5. 凭证明文只允许进入 TLS + CSRF 保护的写请求；协议响应、元数据、日志和浏览器 URL 均不得包含明文或密文。数据库连接只保存 CredentialRef。
6. `platform.admin` 管理全部资源；领域角色按 read/write/rotate/revoke/probe 分离。设置写入仍受 bootstrap-policy 最高优先级保护，只有直接登录的 `platform.admin` 映射为管理员。

## 否决方案

- 单一管理中心插件：会反向依赖全部领域并形成新单体。
- 通用 capability HTTP 代理：浏览器一旦可选目标或操作，新增后端能力会被意外暴露。
- Portal Edge 本地拉起四个服务：破坏 leader、故障转移和多内核集群边界。
- 使用 `api.route` 直接托管页面 API：当前插件 API 规范尚未封板，且会让身份、CSRF、错误映射和审计分散到每个插件。

## 影响

Frontend Platform Profile 固定四个管理插件的精确版本。启用管理 API 的 Portal Edge 必须配置 NATS、能力目录和生产 addressing 传输身份；能力 unit 必须附加平台管理访问策略。任一前置条件缺失时管理调用 fail-closed，Portal 其他功能不受影响。
