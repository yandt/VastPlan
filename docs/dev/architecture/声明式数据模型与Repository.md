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

## 3. 生成器

可信工具 `engineering/tools/datamodelgen` 使用 Go 实现，因为 Manifest、发布门禁和 Database Runtime 的权威契约均以 Go 校验。它按目标插件语言输出独立文件：

```bash
go run ./engineering/tools/datamodelgen \
  -model <plugin>/data-models/example.json \
  -language go|typescript|python \
  -out <plugin>/generated/<language> \
  -package generated
```

生成物必须提交到插件自己的 `generated/` 目录。架构门禁会重新读取签名模型、核对摘要、复生成已存在语言目录并逐字节比较；生成结果不确定、模型被修改但 Manifest 摘要未更新、或提交的生成文件陈旧都会失败。

生成的公共面包括 Create、Get、List、Update、Delete、Batch、乐观锁 revision、幂等键、UnitOfWork 和 Outbox。V1 不生成公开 CRUD API、业务 Workflow、关联查询、任意 Join、报表聚合或 Workbench 页面。复杂查询由插件自己的 Repository Adapter 实现，但仍须通过 Database Runtime 使用参数化 SQL。

## 4. Schema 演进

`schemaVersion` 与插件 SemVer、Capability Contract Version 相互独立：

- 新增表、可空字段和安全索引可以形成自动迁移候选；
- 删除字段、修改类型、收窄约束和数据重写必须附带签名迁移；
- 迁移由单一 Schema Controller 在备份检查、数据库迁移锁和持久账本保护下执行；
- 普通 Repository 或 Workflow 无权自行运行 DDL。

首个真实模型是 Database Capability Pack 中的 `platform.database.connection`。它用于验证签名外部模型、Go/TypeScript 双语言生成和零漂移门禁；其业务工作流迁移属于 P5，不能因为生成了 Repository 类型就宣称状态已经进入 SQL。

## 5. 当前实施状态

- P2 已建立 `data.model.v1` JSON Schema、Go 严格解析与语义校验；
- 已提供 Go、TypeScript、Python 确定性生成器；
- Plugin Manifest 已支持签名 `backend.dataModels[]` 引用；
- Database Connection 模型已作为首个真实样本接入；
- 已建立摘要、路径、模型身份和生成物零漂移门禁。

P3 的第一检查点已冻结独立 `record.store.v1` wire contract、签名模型目录、字段类型转换、可信 tenant/service scope 注入、PostgreSQL/MySQL 参数化 CRUD 编译器、游标分页和 Schema 变更分类器。该检查点尚未把新 capability 注册到 Runtime：只有完成执行适配、幂等账本、Outbox 与 Schema Controller 后才对插件开放。SQL Shared State 仍属于 P3 后续，平台控制数据库 Bootstrap 属于 P4。
