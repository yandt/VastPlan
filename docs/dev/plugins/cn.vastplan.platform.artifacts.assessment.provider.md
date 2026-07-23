# Artifact Security Assessment Provider

该第一方平台插件实现 ADR-0139 的真实运行态扫描端，固定运行于独立 `trusted-process`，不允许 dynamic-go 或共享 Go Runtime。语言选择为 Go 可信编排壳 + 外部 Trivy：Go 负责租约、摘要、受控子进程、规范化与签名；Trivy 提供成熟的多生态漏洞和许可证扫描。Python 更适合未来 ScanCode 适配器，Node.js 对本场景的资源与子进程治理没有优势，Rust 可作为未来更小可信壳但首版工程成本更高。

## 调用链与权限

只有精确的 `cn.vastplan.platform.artifacts.assessment.controller` 认证插件调用者可以执行 `assessAdmission`。Provider 再向 Repository 申请绑定 active ref、tar/SBOM 摘要和本 Provider 受众的 30 秒单次 HTTPS 扫描租约；制品正文不经过协议总线。下载后重复复验 tar、Manifest、版本、publisher 与嵌入式 CycloneDX SBOM。

签名私钥由 `signingKeyRef` 引用，必须同时满足 owner=`cn.vastplan.platform.artifacts.assessment.provider`、purpose=`artifact.assessment.signing-key`、scope=`tenant`。内核只向该精确首方运行身份中继加密 Material Lease；私钥只在一次回调内解析、签名和清零，不进入普通配置、环境 JSON、Repository、Controller、日志或协议响应。

## 扫描与报告

Trivy binary、数据库内容摘要、离线参数和报告 Schema 均由 Provider SDK 强制。`trivySnapshotDirectory` 必须精确指向 `snapshots/<databaseRevision>`，由 File Database Snapshot 基础插件先行原子物化；每次扫描仍验证 binary 版本和 `db/metadata.json + db/trivy.db` 内容摘要，固定启用 filesystem `vuln,license`、JSON、`--offline-scan`、`--skip-db-update`。未知 Schema、空扫描、报告超限、数据库漂移、未知许可证超过策略阈值均 fail-closed。

原始 JSON 通过共享 `reportArchiveDirectory` 写入通用私有内容寻址归档。候选文件先完整同步，再以不覆盖方式原子发布；相同摘要可幂等复用，摘要路径出现不同内容时拒绝。只有归档成功才返回签名记录，Repository 接受 Admission/Status 前还会独立复核报告存在性与摘要。插件为 active-active、queue 路由且不持有调度状态；`assessAdmission` 生成不可变准入，`assessStatus` 生成只追加复扫记录。`status` 只向精确 Controller 返回扫描器、数据库和评估配置 revision，使数据库或策略切换能触发立即复扫。

## 生产要求

- 固定并验证 Trivy binary 版本；数据库由独立更新任务准备不可变快照，扫描请求不得联网更新。
- Repository HTTPS 使用企业信任链；下载禁止重定向并限制 256 MiB。
- Process Supervisor/Guardian 设置 CPU、内存、临时目录与父进程死亡收敛。
- `workRoot`、`reportArchiveDirectory` 仅属主可访问；不得把完整 secret URL 或原始私钥写入日志。
- 代表性插件和真实负载具备前不执行 soak，先通过确定性引擎、真实小样本和有界故障矩阵验收。
