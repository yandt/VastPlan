# ADR-0072 CSP 安全的浏览器 JSON Schema 校验

> 实现更新（2026-07-22）：生产 CSP 现由 Node Portal Kernel 生成；禁止 `unsafe-eval` 的不变量不变，Go Portal HTTP 宿主已经删除。

- 状态：已接受
- 日期：2026-07-18
- 关联：[ADR-0063 Portal 静态宿主与样式隔离](ADR-0063-Portal静态宿主与样式隔离.md)、[ADR-0065 通用 JSON Schema 表单与 Arco 主题适配](ADR-0065-通用JSON-Schema表单与Arco主题适配.md)、[ADR-0066 Arco 按需构建与单文件制品边界](ADR-0066-Arco按需构建与单文件制品边界.md)

## 背景

Portal Edge 的生产 CSP 不允许 `unsafe-eval`。AJV 8 默认通过 `new Function` 编译运行期 Schema；真实浏览器加载动态表单时因此触发 CSP 拒绝。表单仍可能依赖后端校验完成写入，但浏览器同步校验会产生错误并失去可信一致性。

系统还要求功能插件可在运行期提供签名 JSON Schema，因此不能只为当前已知表单预生成固定校验函数。

## 决定

1. 保留 RJSF 6 作为表单树、默认值、组合字段和错误展示引擎；浏览器同步 Validator 改用 `json-schema-library` 11.x 的 Draft 7 解释式校验，并通过 RJSF `ValidatorType` 适配。
2. 禁止为表单开放 `script-src 'unsafe-eval'`。Portal CSP、nonce、Blob 模块白名单和远程 `$ref` 禁令保持不变。
3. 浏览器支持标准 Draft 7 规则和 VastPlan `vastplan-credential-ref` 格式；同步、异步和宿主错误继续投影为统一字段路径。后端仍执行最终 Schema、权限和业务约束校验，浏览器结果不构成授权。
4. 移除 `@rjsf/validator-ajv8` 直接依赖。新增 `json-schema-library` 11.6.1（MIT），锁定版本并继续纳入许可证、漏洞与制品扫描。
5. Metafile 显示解释器主体增加 93,823 字节，URI 支持增加 19,759 字节；压缩后 Arco 单文件为 1,644,870 字节。ADR-0066 的显式预算调整为 1,700,000 字节，仍由按需组件、样式闭包和单文件摘要门禁约束。

## 备选方案

- 在 CSP 加 `unsafe-eval`：实现最小，但允许任意动态代码生成，破坏 Portal 的脚本执行边界，拒绝。
- AJV standalone 预编译：适合固定 Schema，但无法覆盖签名插件在运行期交付的新 Schema，拒绝作为通用入口。
- 自研 Draft 7 校验器：可完全控制体积，但会重复实现 `$ref`、组合、条件、数组和错误语义，长期风险高，拒绝。

## 影响

- 正面：动态表单在严格 CSP 下无控制台错误，不牺牲 Schema 通用性；服务端权威校验边界不变。
- 代价：设计系统单文件比 AJV 基线增加约 123 KiB；RJSF 内部仍可能传递 AJV 工具依赖，但生产校验路径不调用其动态编译器。
- 门禁：浏览器冒烟必须保持无 CSP/eval 错误；构建继续拒绝超过 1,700,000 字节的 Arco 制品。
