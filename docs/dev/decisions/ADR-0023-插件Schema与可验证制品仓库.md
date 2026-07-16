# ADR-0023 插件 Schema 与可验证制品仓库

- 状态：已采纳
- 日期：2026-07-16
- 关联：[ADR-0010 插件服务与部署编排](ADR-0010-插件服务与部署编排.md)、[ADR-0016 单仓与代码目录布局](ADR-0016-单仓与代码目录布局.md)、[ADR-0019 工程规范基线](ADR-0019-工程规范基线.md)、[插件契约与协议](../architecture/插件契约与协议.md)

## 背景

插件清单此前只有设计文本和示例 JSON；运行态 `RegisterContributions` 也只检查扩展点名与 ID。这意味着拼错 Hook phase、加入未定义字段或手工篡改本地制品后，问题会推迟到宿主分发甚至 Node Agent 安装时才暴露。ADR-0010 已确定插件服务须保存「插件包 + manifest + 版本 + channel」，下载必须做 SHA-256 强校验、fail-closed；现在需要先把这个可信入口落为可复用基础设施。

## 决策

1. 新增顶层 `schemas/plugin/v1/`，其中的 JSON Schema 是插件清单、运行态 descriptor 与制品元数据的**唯一契约源**。该目录内的 Go 包只负责将同一份 JSON Schema 嵌入二进制并执行校验；不得另写一套会漂移的规则。
2. 发布时先校验 gzip tar 插件包根目录的 `vastplan.plugin.json`，再写入本地仓库。元数据固定为 `{pluginId, version, channel, sha256, size, object, manifest}`，并由 artifact Schema 校验。
3. `(pluginId, version, channel)` 是不可变键：完全相同 SHA 的重传幂等；不同 SHA 一律拒绝。读取制品时再次校验索引 Schema、对象大小、SHA-256、包内 manifest 及其与索引的绑定，任一不符即 fail-closed。
4. 协议总线在接收 `RegisterContributions` 时也校验 descriptor Schema。发布清单与运行进程是两条不同输入边界，二者均不得绕过验证。
5. 采用 `github.com/santhosh-tekuri/jsonschema/v6` v6.0.2 执行 Draft 2020-12 Schema。它覆盖项目所需 Draft 版本与 `$ref`，近期仍有维护；许可证为 Apache-2.0，符合 ADR-0019 白名单。自写校验虽然短期少一个依赖，但无法可靠覆盖引用、组合与后续多语言契约演进。

## 备选方案

- **只用 Go struct + `json.Unmarshal`**：实现快，却默认接受未知字段，并把 TS/Runner/后端维护成多份规则；否决。
- **发布阶段校验、运行态不校验**：能挡住正常流程，但被错误或不受信进程发送的协议消息仍可污染 Registry；否决。
- **立即做远端对象存储、签名和 Node Agent reconcile**：这些是后续部署编排工作，当前没有身份、密钥与控制面契约；先稳定可验证的本地制品边界，避免伪造「已部署」闭环。

## 影响

- 正面：清单/descriptor/制品索引有同一份机器可执行真源；版本覆盖与磁盘损坏在制品入口或读取点立即失败；下一阶段 Node Agent 可直接消费已验证的 `Repository.Read`。
- 代价：新增一个 Apache-2.0 直接依赖；开发者修改清单或 descriptor 后必须同步通过 Schema 测试。
- 后续：制品签名、远端对象存储、鉴权、bundle/air-gap 分发以及 Node Agent reconcile 仍待单独设计，不能把 SHA-256 误当作发布者身份认证。
