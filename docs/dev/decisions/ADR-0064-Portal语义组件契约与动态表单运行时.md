# ADR-0064 Portal 语义组件契约与动态表单运行时

- 状态：已接受
- 日期：2026-07-18
- 关联：[ADR-0052 前端门户内核与多 UI 设计系统插件](ADR-0052-前端门户内核与多UI设计系统插件.md)、[ADR-0054 跨端体验契约与交互协调](ADR-0054-跨端体验契约与交互协调.md)、[ADR-0063 Portal 静态宿主与样式隔离](ADR-0063-Portal静态宿主与样式隔离.md)

## 背景

Portal 已有可信 ESM 装载、单一设计系统选择和 Shadow DOM 样式隔离，但 `@vastplan/ui-primitives` 只暴露 Page、Panel、Button、Menu 等少数组件，Arco 动态表单也只正确渲染文本与多行文本。若功能插件直接补用 Arco 类型，未来的 MUI Portal、Mobile 原生适配器与测试工具将无法消费同一语义契约；若每个适配器各自实现校验，条件可见性、默认值和错误路径又会产生漂移。

## 决策

1. `@vastplan/ui-primitives` 提供框架无关的 Web 语义组件面：PortalShell、Page/Panel、Stack/Grid、Menu/Breadcrumb/Tabs/CommandPalette、Dialog/Drawer、FormRenderer、FilterBar/Table/Pagination/Descriptions、Status/Empty/Error/Skeleton/Busy、语义图标和主题 token，以及 notify/confirm。公共接口只能使用 React 基础类型和 VastPlan 契约类型，不得暴露 Arco/MUI props。
2. `@vastplan/ui-contract` 继续保存可序列化 `FormSchema`，并提供无 UI、无网络依赖的默认值、路径取值、条件可见性和同步校验运行时。错误使用稳定的 `object.field`、`array[0].field` 路径和错误码；Web、Mobile 与其他工具可复用同一规则。
3. Arco 适配器完整映射 `text`、`textarea`、`number`、`boolean`、`select`、`multiSelect`、`date`、`object`、`array` 和 `secretRef`。表单保持受控输入；修改任意字段时产生新的值对象，不在适配器中保存另一份业务真相。
4. 同步规则留在 Schema；需要服务端或异步查询的校验通过 `FormRenderer.validate` 注入，并携带 AbortSignal、表单 context 和默认 250ms 去抖。函数、网络地址与框架组件不得进入可序列化 Schema。外部校验错误也使用稳定字段路径。
5. `secretRef` 只编辑引用 ID 或从引用选项中选择，不接受密文/明文回填语义。设计系统不负责取得凭证明文。
6. Select 适配器在内部编码选项索引，因而可以保留契约允许的 string/number/boolean 以及混合类型值；编码不得出现在 FormSchema 或提交值中。
7. Arco 的通知与确认使用 Provider 内 hook holder，并与普通 Modal/Drawer 一同挂到当前 Portal 的 Shadow DOM 容器。禁止使用逃逸到 `document.body` 的全局静态 overlay 服务。
8. V1 字段类型为封闭集合。新增通用字段先进入 `ui-contract` 并由所有目标适配器实现；不允许第三方插件注册携带框架私有组件的任意字段类型。未来确有隔离后的自定义渲染需求时另立 ADR。

## 备选方案

- **由 Arco 插件独占校验和字段模型**：实现快，但第二设计系统和 Mobile 会复制规则，否决。
- **直接向功能插件暴露 Arco 全组件 API**：组件丰富，但设计系统不可替换且功能插件可绕过 Overlay/CSS 治理，否决。
- **立即引入通用 JSON Schema 表单框架**：能力更广，但会同时引入另一套 UI/校验抽象和较大依赖，当前十类平台字段不需要该复杂度，暂不采用。

## 结果

- 正面：功能插件只组合稳定语义组件；动态表单规则跨端一致；Arco 可替换；异步校验可取消且不会污染 Schema；overlay 保持 Shadow DOM 隔离。
- 代价：每个新设计系统都必须实现完整组件面；增加字段类型需要同步评估 Web 与 Mobile；当前没有任意第三方自定义字段渲染。
- 验收：TypeScript 类型门禁、纯表单运行时单元测试、Arco 组件面完整性测试、插件真实 ESM 构建必须同时通过。

## 后续实施记录

- 2026-07-23：随着动态表单能力扩展，Arco 与 MUI 均已采用 RJSF 6 作为框架内呈现层。两者不再各自维护校验实现，而是共享 `@vastplan/rjsf-csp-validator` 的 Draft 7 解释式校验器；该 SDK 禁止 `Function/eval`，并统一 VastPlan CredentialRef 与一次性秘密格式。原“立即引入通用 JSON Schema 表单框架”备选项记录的是本 ADR 作出时的阶段判断，不再描述当前实现状态。
