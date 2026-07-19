# ADR-0065 通用 JSON Schema 表单与 Arco 主题适配

- 状态：已接受
- 日期：2026-07-18
- 后续变更：浏览器同步校验器因严格 CSP 改为解释式实现，见 [ADR-0072](ADR-0072-CSP安全JSON-Schema校验.md)；RJSF 与 JSON Schema 契约不变。
- 关联：[ADR-0052 前端门户内核与多 UI 设计系统插件](ADR-0052-前端门户内核与多UI设计系统插件.md)、[ADR-0054 跨端体验契约与交互协调](ADR-0054-跨端体验契约与交互协调.md)、[ADR-0064 Portal 语义组件契约与动态表单运行时](ADR-0064-Portal语义组件契约与动态表单运行时.md)
- 修订：本 ADR 取代 ADR-0064 中关于自研字段集合、表单运行时以及暂不引入通用框架的决定；ADR-0064 的 Portal UI 语义组件边界、异步校验和 Shadow DOM 约束继续有效。

## 背景

自研 `FormField[]` 已覆盖十类基础字段，但继续扩展会重复实现 JSON Schema 已成熟定义的组合、条件、数组、引用、默认值和校验语义，并要求 Web、Mobile、测试工具分别追赶同一套私有规则。系统尚处开发期，可以直接统一契约而不保留历史数据兼容层。

候选方案：

- RJSF Core + AJV + VastPlan Arco Theme：数据规则使用标准 JSON Schema，主题和 widget 可完全替换；
- JSON Forms Core + Arco Renderer：规则与布局能力强，但要求维护专用 UI Schema 和完整 renderer set；
- Formily + Arco：响应式模型丰富，但 `x-component`、`x-reactions` 等私有扩展会把 UI 实现语义带入公共 Schema。

## 决定

1. `FormSchema` 成为一个薄信封：`id + schema + uiSchema?`。`schema` 是唯一数据约束真相，V1 固定为 JSON Schema Draft 7，根类型必须为 `object`；`uiSchema` 只能包含可序列化的呈现提示，不得包含组件、函数、网络地址、身份或秘密值。
2. Web 表单引擎采用 [RJSF](https://rjsf-team.github.io/react-jsonschema-form/) 6.x，校验采用 AJV 8。RJSF/AJV 仅存在于设计系统插件内部；`@vastplan/ui-contract` 与 `@vastplan/ui-primitives` 不导出 RJSF 类型，功能插件仍只依赖 VastPlan 契约。
3. `cn.vastplan.foundation.frontend.render.adapter.arco` 提供 Arco widgets、字段/对象/数组模板、数组操作、错误展示和中文错误转换。其他 Web 设计系统可以复用相同数据 Schema，但必须提供自己的主题适配；一个 Portal 只运行一个设计系统，所以不会同时装载多份主题引擎。
4. `required`、`minLength`、`minimum`、`pattern`、`enum/oneOf`、`items`、`if/then/else`、`readOnly`、`default` 等规则只写入标准 JSON Schema。`uiSchema` 可控制 widget、顺序、帮助和布局，但不得降低 Schema 约束或充当授权依据。
5. 凭证字段使用 `type: string + format: vastplan-credential-ref + writeOnly: true`；Web 可用 `ui:widget: secretRef` 呈现。Broker 只信任数据 Schema 中两项安全标记，只接受 `credential://` 引用，不因 UI widget 名称而放行明文。
6. 表单信封进入 Broker 前同时执行 VastPlan 外层契约校验和内嵌 JSON Schema 编译。只允许本地 `#...` 引用，禁止远程 `$ref`；文档限制为 256 KiB、32 层和 4096 个节点，避免 SSRF 与资源耗尽。浏览器 AJV 不配置异步 Schema loader。
7. 异步服务端校验继续通过 `FormRenderer.validate` 注入，采用去抖与 `AbortSignal` 取消；函数和服务地址不进入 Schema。同步 AJV 错误、异步错误和宿主错误统一为稳定字段路径。
8. Mobile/Runner/非 React 工具消费相同 Draft 7 数据 Schema，可以忽略 Web 专用 `uiSchema` 提示并使用本端默认呈现。若未来升级 Draft 2020-12，必须通过明确的契约版本/方言协商，不能静默改变解释器。
9. 不提供旧 `FormField[]` 兼容适配器：代码、协议样例和测试一次性迁移，删除旧运行时，避免永久维护双模型。

## 依赖与许可

新增直接依赖 `@rjsf/core`、`@rjsf/utils`、`@rjsf/validator-ajv8` 6.7.0，均为 Apache-2.0；其 AJV 8 依赖为 MIT，全部在项目许可证白名单内。选择它们是为了避免继续自研通用 Schema 解析、默认值合并、组合规则和错误树；供应链扫描与锁文件门禁继续适用。

## 后果

- 正面：插件表单获得标准生态、复杂对象/数组/条件/组合校验和跨语言工具链；Arco 仍可替换；公共契约不绑定 React 组件。
- 代价：Arco 设计系统制品增加 RJSF/AJV 体积；`uiSchema` 不是跨端布局真相，移动端需要自己的呈现策略；新增方言前必须做兼容测试。
- 边界：JSON Schema 只负责数据合法性，不负责授权、凭证读取、网络调用或业务事务。第三方 Schema 即使格式合法，也必须经过插件签名、权限和资源限制。
