# ADR-0096 配置入口 Codec 与 YAML 嵌套

- 状态：已采纳
- 日期：2026-07-20

## 背景

Backend 的期望态、Platform Profile、Application Composition 和 Catalog 原先只接受 JSON。JSON 是 Schema、内容摘要、NATS 控制面和插件启动配置的共同表示，适合机器传输与签名；但运维人员维护多服务、多插件组合时，需要以多个可读文件组织配置。

普通插件不能承担内核启动配置的解析：内核必须先解析配置，才能知道要下载、验签和启动哪些插件。这一启动顺序使第三方 Codec 不能进入控制面信任根。

## 决策

1. JSON 仍是内核内部、JSON Schema 校验、内容摘要、NATS 传输和 `VASTPLAN_PLUGIN_CONFIG_JSON` 的唯一规范表示；YAML 只作为本地 config-as-code 文件入口。
2. 在 `core/shared/go/configfile` 建立受内核控制的 Codec 边界。当前内置 JSON 与 YAML Codec；未来可增加 TOML 等首方随内核发行的 Codec，但不能由普通运行时插件接管控制面解析。
3. YAML 使用单键对象 `{ "$include": "relative-file.yaml" }` 表达文件替换。用于数组项时，若目标是数组则原地展开，适合拆分 `units`、`plugins` 等列表；可嵌套最多 16 层、最多 128 个文件。
4. 所有 include 必须解析到根配置文件所在目录树内；绝对路径、逃逸路径、符号链接、循环引用和超出大小限制的文件一律拒绝。
5. YAML 禁止 anchor、alias、merge key、重复 key、自定义/隐式复杂类型和非字符串 key。日期、版本号、ID 等应加引号；数字仅接受 JSON 数字语法。
6. `backend-kernel reconcile -startup-file <file.yaml>` 是本地核心启动入口，保留 `-desired` 兼容别名。所有已有 config-as-code 的 `Parse*File` 与 `validate -file` 共用同一入口；Seed 制品仓库 Profile 也适用。

## 备选方案

- **继续只支持 JSON**：机器处理简单，但大型服务组合可维护性差，拒绝。
- **将 YAML 解析交给普通插件**：出现“先解析才能启动插件”的循环，并扩大供应链信任根，拒绝。
- **YAML 原文进入 NATS 与摘要**：缩进、注释和键顺序会产生无意义版本变化，也无法与现有 JSON Schema 统一，拒绝。
- **允许 YAML 任意标签、锚点和跨目录 include**：配置表达力增加，但审计、可复现性和路径边界显著变差，拒绝。

## 影响

- 运维可按服务与插件拆分 YAML 文件，而运行时和远端控制面无需新增格式分支。
- YAML 语法是受限子集；复杂字符串必须显式加引号。
- 后续 Codec 扩展属于内核 Bootstrap 能力；第三方插件只能提供导入、导出或编辑体验，不能成为权威控制面解析器。
