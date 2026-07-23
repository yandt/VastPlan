# Trivy Database File Snapshot

`cn.vastplan.platform.artifacts.assessment.database.file` 是 ADR-0141 的首个扫描数据 Source/Materializer。它选择 Go 独立进程，因为职责是有界文件复制、摘要与原子目录发布；Node/Python 没有相关生态优势。插件按每个 Backend 内核本地运行，物化结果是可重建缓存，不是集群权威。

## 输入与输出

- `sourceDirectory` 是部署适配器准备的私有 staging，必须包含普通文件 `db/metadata.json` 与 `db/trivy.db`。
- `databaseRevision` 是对规范相对路径和文件字节计算的 SHA-256，由可信配置钉死，不从 staging 自报。
- `snapshotRoot` 保存不可变结果，成功目录固定为 `snapshots/<databaseRevision>`。
- `status` 只返回 ready、revision、文件数和总字节数，不返回 staging 或 snapshot 物理路径。

插件在 ACTIVATE 阶段复制到同文件系统 candidate，限制 metadata 为 4MiB、数据库为 2GiB，拒绝符号链接、特殊文件、空文件、宽权限目录及摘要漂移。文件与目录同步完成后才原子 rename；已有 revision 必须重新复核，不能覆盖。失败不会改变任何已发布 snapshot。

## 运行边界

插件不访问互联网、不持有 Registry 凭证、不运行 Trivy 扫描，也不解析漏洞内容。当前 File Source 适合本地测试、离线导入和企业部署适配器；未来 OCI/S3 Source 负责生成相同 staging，不能改变 Provider 只读精确 revision 的规则。

Assessment Provider 的 `trivySnapshotDirectory` 必须精确指向该插件输出的 `snapshots/<databaseRevision>`，并在每次扫描前再次计算摘要。数据库换代通过新配置 generation 完成，不使用可变软链接。
