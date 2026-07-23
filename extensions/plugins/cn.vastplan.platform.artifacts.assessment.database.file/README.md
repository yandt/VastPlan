# Trivy Database File Snapshot

该每内核基础插件只负责把私有本地 staging 的 `db/metadata.json + db/trivy.db` 原子物化为 `snapshotRoot/snapshots/<databaseRevision>`。它不联网、不解析漏洞内容、不运行扫描，也不把物理路径暴露给 Portal。

权威设计见 [ADR-0141](../../../docs/dev/decisions/ADR-0141-扫描数据库不可变快照与评估报告归档.md)。
