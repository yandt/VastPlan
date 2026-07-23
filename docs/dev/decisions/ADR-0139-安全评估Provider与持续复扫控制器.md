# ADR-0139：安全评估 Provider 与持续复扫控制器

- 状态：已采纳，分阶段实施
- 日期：2026-07-24
- 关联：[ADR-0093](ADR-0093-可信宿主加密Material-Lease.md)、[ADR-0094](ADR-0094-操作系统Guardian与独立进程故障收敛.md)、[ADR-0128](ADR-0128-统一Leader-Epoch与外部副作用Fencing.md)、[ADR-0138](ADR-0138-插件安全准入与可追加复扫状态.md)

## 背景

ADR-0138 已经完成不可变准入记录、只追加复扫链、仓库与 Node 双端复验，但真实扫描仍需要解决四个不同问题：安全工具如何执行、制品大对象如何交付、签名私钥如何使用、集群中谁负责定时复扫。把它们塞进仓库或内核会让漏洞数据库、第三方解析器和高资源扫描任务进入可信常驻路径；把全部职责放进一个扫描插件，又会让持久调度状态与高价值私钥共享过大的故障域。

## 决策

采用“独立 Security Assessment Provider + 无密钥 Rescan Controller + 仓库短期扫描租约”的三段式设计。

1. `cn.vastplan.platform.artifacts.assessment.provider` 是第一方、独立可信进程。它只接受精确制品身份、只读短期扫描租约、选中的策略 ID 和复扫链位置，下载并复验制品与 SBOM 后执行扫描，规范化结果并签署 `AdmissionRecord` 或 `StatusRecord`。
2. Provider 不进入 Go 共享 Runtime，也不进入内核进程。它持有 Provider 签名私钥、启动受控扫描器子进程并处理不可信报告，必须由 Process Supervisor/Guardian 管理，具有独立资源限额、临时目录和网络策略。
3. `cn.vastplan.platform.artifacts.assessment.controller` 是 leader-owned 集群控制器。它枚举需要复扫的精确 ref，持久化 `nextScanAt/attempt/lastSequence/lastDigest/databaseRevision`，使用 leader epoch fencing 发起扫描并追加状态。它不持有签名私钥，也不接触明文密钥。
4. Repository 只向经过可信运行身份认证的 Provider 签发一次性、短 TTL、只读、绑定精确 ref、SHA-256、HTTP 方法和受众的扫描租约。制品正文不经过协议总线；租约不能列目录、发布、读取其他 ref 或追加复扫状态。
5. Provider 私钥通过托管 `CredentialRef` 和加密 Material Lease 交给精确运行实例。内核只按“可信发布者 + 明确插件 ID + 明确 credential owner/purpose”策略中继密文，不接受普通插件自报权限，也不把明文返回控制器或仓库。
6. 扫描引擎由 Provider SDK 接口隔离。首个实现使用 Trivy filesystem JSON，同时启用漏洞和许可证扫描；生产默认 `--offline-scan --skip-db-update`，数据库由独立更新任务下载、验证并原子切换只读快照。OSV-Scanner、ScanCode 和商业 SCA 后续可新增适配器，不改变内核或评估记录。
7. 原始报告必须有界并外置归档，规范记录只保存 SHA-256。扫描器路径和参数不是任意 shell 字符串：只允许规范绝对二进制路径、固定参数模板和受控枚举；执行不经过 shell，超时后终止整个任务进程组，临时目录仅属主可访问并在结束后清理。
8. Controller 使用到期时间减去安全窗口生成下一次复扫时间，并加入按 ref 稳定抖动，避免整点风暴。临时失败按有界指数退避重试；签名、身份、链位置、策略或制品摘要错误属于永久失败并告警，不做无限重试。
9. 同一 ref 同一 sequence 只允许一个 fenced 调度所有者。Provider 的幂等键绑定 `ref + admissionDigest + sequence + databaseRevision + policyID`；重复执行可以返回同一结果，数据库 revision 或策略改变则产生下一 sequence，不能覆盖既有状态。
10. 数据库快照不可用、扫描器版本不匹配、没有识别到任何包、报告超限、许可证分类未知超过阈值、租约过期或归档失败时均 fail-closed。没有真实负载时不做 soak；先以确定性假扫描器、故障矩阵和小型真实样本验证，待代表性插件进入仓库后再做持续负载测试。

## 语言与运行方式选型

### Go（采用）

适合实现可信编排壳：静态单文件、低常驻资源、严格 JSON/摘要/Ed25519 支持成熟，`context` 与 `os/exec` 便于超时和子进程收敛。扫描本身继续由成熟外部工具完成，避免把大型扫描生态编译进 Provider。

### Python

报告解析与安全工具生态强，适合未来 ScanCode 专用适配器；但作为持有密钥的常驻编排壳会增加解释器、依赖环境和供应链面，首版不采用。

### Node.js

配置与网络服务开发快，但对高资源子进程治理、常驻内存和安全扫描生态没有明显优势，首版不采用。可用于无密钥 Portal 管理面，不用于签名 Provider。

### Rust、Java、商业服务

Rust 可进一步缩小资源和内存安全风险，但首版工程成本更高；Java 适合已有企业 SCA SDK；商业服务通过相同 Provider 协议接入。它们均不要求修改内核。

运行方式与语言分开决策：Provider 固定为独立可信进程；Controller 可使用 Go 独立插件进程。即使二者均使用 Go，也不得合并进程或进入 dynamic-go。

## 实施顺序

1. 先交付可复用 Provider SDK、Trivy 离线适配器、严格报告规范化和本地/CI 入口。
2. 再交付 Repository 扫描租约与 Provider 的 Material Lease 签名入口，完成真实独立进程插件。
3. 随后交付有 fenced leader、持久计划、抖动、退避和幂等键的复扫 Controller。
4. 最后接入 Portal 配置、数据库快照状态、报告归档与企业故障矩阵；代表性插件具备真实负载后再执行 soak。

## 否决方案

- **扫描器编译进内核或仓库**：扩大可信计算基座并把数据库更新绑定核心发布，否决。
- **Provider 与 Controller 合并**：调度状态、仓库写权限与签名私钥集中，爆炸半径过大，否决。
- **制品正文通过插件 RPC**：大对象占用控制面消息、内存和超时预算，否决。
- **在线扫描器自行更新数据库**：生产扫描结果不可复现且会在扫描请求路径引入外网，否决。
- **首版同时强绑多个扫描器**：报告合并、安装和故障面不必要地放大；保留适配接口但默认单引擎，否决。

## 实施记录

- 2026-07-24：完成 Provider SDK、Trivy 离线适配、本地/CI 入口、Repository 单次扫描租约、精确 Material Lease 授权和独立 active-active Provider。Provider 0.2.0 增加 StatusRecord 与数据库/评估配置 revision；0.3.0 按 ADR-0141 改为只读精确不可变 snapshot，并使用共享内容寻址报告归档。
- 2026-07-24：完成 leader-owned Rescan Controller。计划按 ref 分 key 保存于 fenced Shared State，Provider 记录先持久为 pending 再追加；后台循环通过 ADR-0140 的 Manifest 权限和宿主绑定 tenant 上下文发起 HostCall，Runtime Host 在 leader lease 丢失后阻止外部 HostCall，Repository 只追加链继续作为竞态最终 CAS。
- 2026-07-24：真实子进程 E2E 已覆盖 Controller 激活后自主调用、Provider/Repository capability 链、Shared State fenced CAS 与最终计划提交；确定性故障测试覆盖追加暂时失败复用 pending、追加成功但最终计划保存失败后的 Catalog 链头恢复，以及数据库 revision 变化立即生成下一 sequence。该验证不包含 soak。
- 2026-07-24：ADR-0141 已完成通用在线配置语义、不可变 File Snapshot、共享报告归档、Portal 数据库 revision 概览与独立报告数据面。短时企业故障矩阵覆盖快照摘要/符号链接、报告篡改/权限、普通与报告 Ticket 跨路径、Controller pending/恢复、真实插件进程和 Portal 路由；目标企业网络、真实 Vault/CA/扫描器和 SLA 仍需现场验收，soak 继续延期。
