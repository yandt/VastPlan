# ADR-0066 Arco 按需构建与单文件制品边界

- 状态：已接受
- 日期：2026-07-18
- 关联：[ADR-0062 Frontend 可信 ESM 制品与运行描述](ADR-0062-Frontend可信ESM制品与运行描述.md)、[ADR-0063 Portal 静态宿主与样式隔离](ADR-0063-Portal静态宿主与样式隔离.md)、[ADR-0065 通用 JSON Schema 表单与 Arco 主题适配](ADR-0065-通用JSON-Schema表单与Arco主题适配.md)

## 背景

Arco 设计系统插件最初从 `@arco-design/web-react` 顶层入口导入组件，并把完整 `dist/css/arco.css` 作为文本装入已签名 ESM。即使构建器执行 tree shaking，Arco 的副作用标记仍会保留部分未使用组件；完整 CSS 还包含 Portal 从未使用的组件样式。引入 RJSF/AJV 后，该单文件制品达到 1,854,018 字节，继续增加组件会产生无边界的体积增长。

候选方案：

- 编译期按需：组件、图标使用直接 ESM 入口，只合并实际组件的传递样式闭包，仍输出单文件；
- 运行时分块：组件首次使用时动态加载独立 JS/CSS chunk；
- Portal 共享 Arco vendor：由内核 import map 提供框架依赖，设计系统插件只保留适配代码。

## 决定

1. V1 采用编译期按需构建。所有 Arco 组件和图标集中在 `arco-components.ts`，组件必须使用 `@arco-design/web-react/es/<Component>` 直接入口；禁止顶层 Barrel 和完整主题 CSS。
2. `arco-styles.ts` 是组件清单的传递样式闭包。基础 token/reset、内部依赖和组件自身样式作为文本合并，继续由设计系统 Provider 注入 Portal Shadow DOM；CSS 不成为独立的未验签网络资源。
3. 构建后执行 `engineering/tools/check-arco-on-demand.mjs`：从每个组件的 `style/css.js` 递归计算预期样式，要求声明集无缺失、无残留，拒绝全量入口，并限制压缩后的单文件制品不超过 1,600,000 字节。依赖升级若确需突破预算，必须先分析 metafile 并显式调整预算。
4. 空态使用轻量 `Empty`，不使用会静态带入多套状态插画的 `Result`。语义 UI 契约不因此变化。
5. 暂不采用运行时分块。当前可信加载协议只锁定一个 `entry.frontend` 字节摘要；在 Manifest、ArtifactVerifier、RuntimeSpec 与浏览器 Loader 能共同锁定模块图、每个 chunk 摘要和失败回滚前，不允许设计系统自行发起相对 chunk 加载。
6. 不把 Arco 提升为 Portal 共享 vendor。React 和 VastPlan UI 契约是跨插件单例，Arco 是可替换设计系统的私有实现；将其放入内核 import map 会让内核绑定特定 UI 框架并妨碍 MUI 等并行适配器。

## 后果

- 正面：保持单文件验签和 Shadow DOM 样式隔离的同时，基线制品降至 1,521,687 字节，减少 332,331 字节（约 17.9%）；新增组件遗漏样式和重新引入全量包会在构建期失败。
- 代价：组件清单和样式闭包是显式工程资产；Arco 版本升级可能改变内部样式依赖，需要由校验器和契约测试共同验证。
- 演进：未来若页面级延迟加载收益明显，应先扩展可信多文件前端制品协议，再引入动态 `import()`；不能绕开统一制品签名、内容摘要和激活 revision 约束。
