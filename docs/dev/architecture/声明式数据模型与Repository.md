# 声明式数据模型与 Repository

本文是 `data.model.v1`、Repository 生成物和应用工作流分层的实现真源。平台控制数据库和两阶段启动的架构决策见 [ADR-0194](../decisions/ADR-0194-平台控制数据库与声明式数据分层.md)，数据库执行面见 [Database Runtime 基础插件](../plugins/cn.vastplan.foundation.data.relational.runtime.md)。

## 1. 固定分层

```text
DataModel 数据定义
        ↓ 构建期生成
Repository Port / Client
        ↓ 由应用显式依赖
Application Workflow
        ↓
Workbench Action / API / 跨插件调用
```

DataModel 只描述持久化结构，不能包含动作、路由、权限或页面。Repository 只负责参数化读写、分页、排序、CAS、幂等、事务和 Outbox，不作授权或业务状态判断。Application Workflow 拥有业务规则、权限调用、事务边界和副作用编排；即使只有一次简单写入，也不能由页面或 HTTP Handler 直接调用 Repository。

## 2. 签名模型

插件把模型放在自身 `data-models/*.json` 中，Manifest 的 `backend.dataModels[]` 只声明：

- 稳定模型 ID；
- 精确 `data.model.v1` 契约版本；
- 插件目录内的相对路径；
- 文件 SHA-256。

模型声明表名、字段与列映射、主键、索引、唯一约束、tenant/service 作用域、敏感级别、乐观锁、审计字段和删除策略。`storage.kind=platform-control` 只表示该模型有资格请求平台控制存储绑定，不代表插件可以读取保留连接；最终授权仍来自可信 Platform Profile。

`data.model.v1` 当前契约版本为 `1.1.0`。所有进入主键、索引或唯一约束的 `string` 字段必须声明 `maxLength`：PostgreSQL 可继续使用受约束文本语义，MySQL 方言据此生成有界 `VARCHAR`，防止组合索引超过引擎上限。Wire `int64` 始终使用十进制字符串，因此 TypeScript 生成类型也是 `string`，不能在 Repository 边界提前转换为可能丢失精度的 JavaScript `number`。

## 3. 生成器

可信工具 `engineering/tools/datamodelgen` 使用 Go 实现，因为 Manifest、发布门禁和 Database Runtime 的权威契约均以 Go 校验。它按目标插件语言输出独立文件：

```bash
go run ./engineering/tools/datamodelgen \
  -model <plugin>/data-models/example.json \
  -language go|typescript|python \
  -out <plugin>/generated/<language> \
  -package generated
```

生成物必须提交到插件自己的 `generated/` 目录。生成器把 Manifest 锁定的模型 SHA-256 同时写入 Go、TypeScript 或 Python 常量，使自定义 Repository Adapter 不必再复制摘要字符串。架构门禁会重新读取签名模型、核对摘要、携带同一摘要复生成已存在语言目录并逐字节比较；生成结果不确定、模型被修改但 Manifest 摘要未更新、或提交的生成文件陈旧都会失败。

生成的公共面包括 Create、Get、List、Update、Delete、Batch、乐观锁 revision、幂等键、UnitOfWork 和 Outbox。V1 不生成公开 CRUD API、业务 Workflow、关联查询、任意 Join、报表聚合或 Workbench 页面。复杂查询由插件自己的 Repository Adapter 实现，但仍须通过 Database Runtime 使用参数化 SQL。

## 4. Schema 演进

`schemaVersion` 与插件 SemVer、Capability Contract Version 相互独立：

- 新增表、可空字段和安全索引可以形成自动迁移候选；
- 删除字段、修改类型、收窄约束和数据重写必须附带签名迁移；
- 迁移由单一 Schema Controller 在备份检查、数据库迁移锁和持久账本保护下执行；
- 普通 Repository 或 Workflow 无权自行运行 DDL。

签名迁移的真源是插件制品而不是单独的临时密钥：插件在 `data-migrations/*.json` 交付 `data.migration.v1`，Manifest `backend.dataMigrations[]` 固定其路径、版本边和 SHA-256，整个 Manifest 再由制品供应链验签。可信组合器只能携带已验证 Plugin Inventory digest 和 Artifact SHA 同步目录；Runtime 要求目标模型与迁移属于同一插件、同一 Artifact，并按 generation 原子切换。

破坏性迁移必须显式声明 `requiresBackup=true`、`requiresApproval=true` 和 `retrySafe=true`。Schema Controller 只接受受限单条 SQL 列表，禁止注释、多语句、事务控制和 `vastplan_*` 内部表；执行前必须同时验证当前连接 leader、备份完成与审批完成的宿主 evidence。账本成功记录模型版本、摘要和 migration ID；失败不会推进目标版本，调用方只能在恢复检查后重试同一已签名、声明可重放的迁移或从备份恢复。

首个真实模型是 Database Capability Pack 中的 `platform.database.connection`，用于验证签名外部模型、Go/TypeScript 双语言生成和零漂移门禁。`platform.interaction.record` 是首个完成业务迁移的增长型模型：Interaction Broker 通过通用 Go Record Store 协议客户端和本插件自有 Adapter 执行逐记录 CAS，Workflow 不依赖 SQL、驱动或具体 Provider。

## 5. 当前实施状态

- P2 已建立 `data.model.v1` JSON Schema、Go 严格解析与语义校验；
- 已提供 Go、TypeScript、Python 确定性生成器；
- Plugin Manifest 已支持签名 `backend.dataModels[]` 引用；
- Database Connection 与 Interaction Record 模型已接入；
- 已建立摘要、路径、模型身份和生成物零漂移门禁。

P3b 已在 Database Runtime 内部实现 `record.store.v1` 的 CRUD、受限分页、Batch、实例亲和 UnitOfWork、幂等账本、事务内 Outbox，以及 PostgreSQL/MySQL Schema Controller 和 SQL Shared State。Schema Controller 只自动执行建表、增加可空字段和非唯一索引，并同时要求可信 SYSTEM 调用、Schema Controller credential evidence、数据库迁移锁和持久账本；其他变化保持 manual。SQL Shared State 延续既有 `sharedstate.Store` 的 1 MiB、CAS revision、游标分页、tenant/service 隔离和 fail-closed 语义。

这些模块已经进入 Database Runtime 主入口和公开 Manifest。Controller 从同一 Deployment 的精确制品生成宿主保留 Inventory，Node Agent 在候选路由发布前同步模型目录；Platform Control Bootstrap 在双 binding 切换前完成安全 Schema 准备。普通插件只使用 `extensions/sdk/go/recordstore` 等协议客户端，具体 Repository Adapter 仍归插件所有，SDK 不拥有 Provider、Engine 或业务 Repository 实现。

`engineering/tools/database-fault-matrix.sh` 是 PostgreSQL/MySQL 真实验收的唯一入口。除 Provider、Record Store、故障注入外，它还验证两个 Runtime 副本并发初始化，以及全部连接池关闭、Registry 重建后的 Platform Control 状态恢复。Docker daemon 未在五秒内响应时脚本立即失败，不把“测试未执行”误报成通过。
