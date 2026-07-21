# ADR-0063 Portal 静态宿主与样式隔离

> 实现更新（2026-07-22）：静态宿主约束已由 Node Portal Kernel 实现，参数使用 `--portal-assets`；Go Portal HTTP 静态宿主已经删除。开发网关只保留独立的内存快照适配器用于 HMR。

- 状态：已采纳
- 日期：2026-07-18
- 关联：[ADR-0052 前端门户内核与多 UI 设计系统插件](ADR-0052-前端门户内核与多UI设计系统插件.md)、[ADR-0062 Frontend 可信 ESM 制品与运行描述](ADR-0062-Frontend可信ESM制品与运行描述.md)

## 背景

可信 ESM Loader 解决了插件模块执行前的来源与字节完整性，但 Portal Shell 本身仍需要可部署宿主、共享依赖 import map、CSP 和样式隔离。若把全部 Node 产物编译进 Backend Go 二进制，前端框架升级会改变内核二进制并显著增大制品；若一开始强制外部 CDN，又会引入额外域名、缓存、CORS 和部署一致性问题。

设计系统 CSS 也不能直接进入主文档。即使当前只允许第一方插件，全局 reset、`body` token 和 overlay 容器仍可能污染嵌入 Portal 的其他产品页面。

## 决策

1. Portal Shell 构建为独立静态目录，不编译进 Backend Go 二进制。`build-frontend.sh` 生成 `index.html`、Portal Kernel 和 `react/react-dom/@vastplan/ui-primitives/@vastplan/ui-contract` 单例 vendor ESM；插件 ESM 仍进入各自签名插件包。
2. 首期由 Portal Edge 通过必填 `-portal-assets` 提供静态目录。目录在启动时一次性读取到有界内存，只接受普通文件，拒绝符号链接、目录列表、超量文件和超量字节；运行期间磁盘替换不会改变已启动进程提供的内容。
3. `/v1` 与所有未知 `/v1/*` 永不回落到 SPA。`/assets/*` 只读取启动时登记的精确文件；其他 GET/HEAD 路径返回 `index.html` 以支持客户端路由。
4. `index.html` 必须包含构建时 nonce 占位符。Edge 每次响应生成随机 nonce 并施加 CSP：脚本只允许同源、该 nonce 与 Loader 所需的 `blob:`；禁止 object、frame ancestor、worker 和非同源连接。Arco/React 当前使用 style 属性和运行时 style 元素，因此 `style-src` 暂时保留 `'unsafe-inline'`，但不得把该例外扩到 `script-src`。静态文件带 `nosniff`、同源资源策略与内容 ETag。
5. Portal Kernel 把 React 根挂载到一个 Shadow DOM。设计系统模块把受治理 CSS 文本作为已验签 JavaScript 的一部分交付，因此模块 SHA-256 同时约束组件代码和样式；仅把 CSS 的文档根选择器 `html/body/*` 改写为 `:host`，普通 Arco 选择器依靠 Shadow DOM 自然隔离。
6. Overlay 容器由设计系统 Provider 固定到 Shadow DOM 内部。功能插件不获得 shadow host、原始 CSS 注入端口或顶级 DOM 控制权。
7. 静态 vendor 使用稳定 URL、私有 `no-cache` 与 ETag，部署升级可立即重验证；revision 插件模块继续使用带摘要的不可变缓存。未来迁移 CDN 时必须保留同源或等价 CSP/SRI、原子版本目录和回滚语义。

## 备选方案

- **把静态产物 `go:embed` 进 Backend**：单文件部署简单，但 UI 依赖会污染 Backend 内核发布与 SBOM，否决。
- **首期由独立 CDN 托管**：扩展性好，但当前增加跨域、缓存原子性和部署凭据，延后；Edge 静态端口保持可替换。
- **设计系统 CSS 放入 Portal Shell**：会把 Arco 固化进内核，否决。
- **设计系统向主文档注入全局 CSS**：可能污染宿主页和不同 Portal，否决。
- **每个插件自带 React**：破坏 Context、hooks 与 Portal UI 单例，否决。

## 影响

- 正面：Portal Shell、共享运行时和插件制品形成清晰的三层发布边界，Backend 内核不包含 UI 框架字节。
- 正面：CSP、Shadow DOM、受控 overlay 与摘要校验共同限制前端插件的全局副作用。
- 代价：正式部署需要同时交付 Backend 二进制、Portal 静态目录和签名插件包。
- 代价：第三方前端插件仍不能进入主 Shadow DOM；未来必须增加 iframe/Realm 隔离与消息契约。
- 约束：Portal 静态目录缺失、nonce 模板无效或包含非常规文件时，Portal Edge 必须启动失败，不能退化为无 CSP 页面。

## 实施澄清（2026-07-19）

文档根映射必须同时覆盖格式化与压缩 CSS；`body{...}`、`html,body{...}` 和 `body[arco-theme=...]` 分别映射为 `:host` 或 `:host(...)`。生产构建门禁应直接以压缩后的 Arco 基础样式调用同一映射函数，避免开发态测试通过而发布制品丢失主题变量。
