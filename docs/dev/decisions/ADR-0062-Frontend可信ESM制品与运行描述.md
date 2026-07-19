# ADR-0062 Frontend 可信 ESM 制品与运行描述

- 状态：已采纳
- 日期：2026-07-18
- 关联：[ADR-0026 远端制品仓库与供应链信任](ADR-0026-远端制品仓库与供应链信任.md)、[ADR-0049 制品信任基座与仓库基础插件](ADR-0049-制品信任基座与仓库基础插件.md)、[ADR-0052 前端门户内核与多 UI 设计系统插件](ADR-0052-前端门户内核与多UI设计系统插件.md)、[ADR-0059 Frontend 双输入采用服务端权威解析](ADR-0059-Frontend双输入服务端权威解析.md)

## 背景

Portal Composer 已能生成带输入摘要和插件来源锁的 `PortalSpec`，但浏览器仍不能据此直接读取 Manifest、访问制品仓库或相信远程模块自行声明的签名与发布者身份。前端代码一旦执行，事后校验已经失去意义；只校验完整插件包的摘要也不能证明浏览器实际执行的 JavaScript 与发布结果一致。

ADR-0052 选择了运行时前端模块，并把 Module Federation 作为初始实现方向。本阶段进一步验证后发现，安全边界真正需要的是“精确制品引用、可复核的模块字节摘要、共享依赖约束和宿主控制的注册接口”，并不依赖某个打包器私有运行时。V1 因此收窄为浏览器原生 ESM 协议；本 ADR 仅替代 ADR-0052 中对 Module Federation 的具体实现选择，不改变其微内核、设计系统插件和在线组合决策。

## 决策

1. `entry.frontend` 必须指向签名插件包内已构建的单文件 `.js` 或 `.mjs` ESM 入口。源码目录、开发服务器地址、运行时 TypeScript 转译和任意外部 URL 都不能成为生产入口。
2. 发布工具通过显式 `-frontend-bundle` 把构建产物写入 Manifest 指定路径，再由既有插件包签名、摘要和不可变版本规则覆盖；Frontend 不建立第二套制品仓库或签名格式。
3. 浏览器只从认证后的 Edge 获取 `RuntimeSpec{portal, modules}`。每个模块描述锁定插件 `id/version/channel`、包摘要、包内入口、同源 URL 和入口 JavaScript 的 SHA-256；浏览器不接收原始 Manifest、仓库地址、对象键、验签密钥或发布者证明。
4. Edge 每次生成运行描述或读取模块时，都从 `ArtifactSource` 取得未信任 Envelope，并经内核 `ArtifactVerifier` 重新验证完整包、证明、发布者和 Manifest 绑定。模块摘要由 Edge 对包内实际 JavaScript 字节计算，不能使用插件自报值。
5. 模块端点只服务当前租户已发布且激活 revision 中锁定的插件，不接受调用方指定版本、channel 或包内路径。响应同源、`nosniff`，并返回模块摘要和包摘要；历史或非激活 revision 不形成任意制品读取接口。
6. Portal Loader 先获取字节并用 Web Crypto 复算 SHA-256，匹配后才通过 Blob URL 执行。插件导出的 provenance 被忽略；`signed/firstParty/integrity` 只能由可信宿主依据 RuntimeSpec 赋值。
7. 功能模块只获得按插件创建的窄注册上下文。宿主写入真实 plugin ID，路由必须唯一且带 React 组件，菜单只能指向本插件已注册路由，防止插件伪造其他插件身份或跨插件挂接菜单。
8. React、React DOM、`@vastplan/ui-primitives` 和 `@vastplan/ui-contract` 由 Portal Shell 作为单例共享依赖提供。V1 使用浏览器 import map；后续可以换成 Module Federation 或其他装载器，但不得改变 RuntimeSpec、字节摘要先验校验和宿主注册边界。

## 备选方案

- **浏览器直接读取签名包并自行验签**：会把仓库访问、信任根、压缩包资源限制和发布者策略复制到浏览器，扩大攻击面，否决。
- **只依赖 HTTPS/SRI 响应头**：服务端或插件仍可能把未锁定版本映射到相同 URL，且动态模块的来源身份不清晰，否决。
- **插件模块自行导出 provenance**：不可信代码可以声明自己已签名或属于第一方，否决。
- **把所有插件编入 Portal Shell**：失去在线组合与独立制品升级能力，否决。
- **立即固化 Module Federation runtime**：能解决共享依赖，但增加打包器特定元数据，且不能替代包级与字节级信任验证；保留为可替换实现而非 V1 wire 契约。

## 影响

- 正面：从发布包到浏览器实际执行字节形成闭环，组合来源、包摘要、模块摘要和激活 revision 均可追溯。
- 正面：装载协议不绑定 Webpack/Vite；未来改变构建器不需要改变 Edge 与插件仓库契约。
- 正面：浏览器插件不能自行提升 provenance，也不能把模块端点当作任意仓库浏览器。
- 代价：Portal Shell 必须维护共享依赖 import map，并把设计系统 CSS 作为受治理资产处理。
- 代价：每次冷启动需要获取并计算模块摘要；可利用带摘要的不可变私有缓存优化，但不能跳过校验。
- 约束：V1 仍只执行经 Catalog 判定为第一方的前端插件。未来第三方 iframe/Realm 隔离应复用 RuntimeSpec，但不得直接获得当前主文档的 Blob 执行权限。
