# Artifact Assessment Provider SDK

该 Go SDK 实现 ADR-0138/0139 的扫描 Provider 侧公共能力，不属于内核。它负责：

- 复核 tar.gz、清单、嵌入式 CycloneDX SBOM 与请求摘要的精确绑定；
- 在仅属主可访问的临时目录安全展开普通文件；
- 通过可替换 `Engine` 执行扫描并规范化漏洞、许可证发现；
- 按策略阈值生成 pass/fail，并用 Ed25519 签署 `AdmissionRecord v1`；
- 返回与记录摘要绑定的原始报告，由 CI 或企业归档服务外置保存。

首个 `Engine` 是 Trivy filesystem JSON。它不允许 shell 命令模板，只接受规范绝对 binary/cache 路径，固定使用 `vuln,license`、JSON、`--offline-scan`、`--skip-db-update`，每次扫描都验证 binary 版本并复核 `db/metadata.json + db/trivy.db` 内容摘要。未知报告 Schema、没有识别到包/许可证、报告超限、数据库摘要漂移均 fail-closed。cache 根目录可供 Trivy 写临时缓存，但 `db` 子目录必须由更新任务以不可变快照方式准备和切换。

## 本地或 CI 使用

先由独立更新任务准备 Trivy cache。不要让扫描请求在线更新数据库。计算要写入 Provider 配置和评估记录的快照 revision：

```bash
go run ./engineering/tools/artifactassessment \
  -print-database-revision \
  -trivy-cache /srv/vastplan/security/trivy-cache
```

生成独立 Provider Ed25519 key，并限制文件权限：

```bash
openssl genpkey -algorithm ED25519 -out /srv/vastplan/security/assessment-provider.pem
chmod 600 /srv/vastplan/security/assessment-provider.pem
```

扫描最终插件包：

```bash
go run ./engineering/tools/artifactassessment \
  -package /tmp/plugin.tar.gz \
  -channel testing \
  -policy testing-default \
  -provider security.vastplan \
  -key-id release-2026 \
  -private-key /srv/vastplan/security/assessment-provider.pem \
  -trivy /usr/local/bin/trivy \
  -trivy-cache /srv/vastplan/security/trivy-cache \
  -scanner-version 0.72.0 \
  -database-revision '<64位快照摘要>' \
  -work-root /srv/vastplan/security/work \
  -report /tmp/plugin.trivy.json \
  -output /tmp/plugin.admission.json
```

默认门禁为 critical/high/未知漏洞、非白名单/未知许可证均为 0；medium/low 不治理。使用 `-max-*` 调整，`-1` 表示该项不治理。工具总是先原子写入报告与记录；判定为 fail 时随后返回非零退出码，使 CI 失败但保留审计证据。

## 安全边界

当前入口适用于本地和 CI。运行态插件尚不能把制品正文塞进协议总线，也不能通过普通配置传私钥；下一阶段必须先完成 Repository 一次性扫描租约和面向该精确首方 Provider 的 Material Lease 授权，再复用本 SDK 包装独立进程插件。
