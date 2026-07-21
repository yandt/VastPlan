# ADR-0101 离线 Bootstrap Inventory 与 LKG 推进

- 状态：已采纳，第一阶段实施
- 日期：2026-07-21
- 关联：[ADR-0097](ADR-0097-测试制品仓库与前端分级热升级.md)、[ADR-0100](ADR-0100-制品生命周期引用保护与垃圾回收.md)

## 背景

Seed Repository 必须能在控制面、NATS 和全部插件不可用时独立启动。让 Seed 服务主动调用 Managed Repository 会形成自举环；让 Managed Repository 扫描 Seed 私有目录又会跨越存储边界。另一方面，GC 和仓库自升级必须知道哪些精确制品仍是空节点自举集合、哪些是已验证的最后可用仓库栈。

## 决策

1. 部署/升级控制器生成 root-owned `Bootstrap Inventory v1`。清单只含 `repositoryId`、单调 generation、全量 Seed 精确 ref+SHA 和 LKG 子集，不含令牌、密钥、目录或任意运行参数。
2. Seed 服务不读取也不发布该清单，继续保持文件系统 + HTTPS 的独立最小服务。拥有 Seed 源的 Backend 内核在 Node Agent 收敛后逐项从 Seed 读取、重新验签并比对 SHA，随后才通过 addressing 发布 `seed/<repositoryId>` 与 `lkg/<repositoryId>` 完整快照。
3. 调用身份固定为 `SYSTEM bootstrap-inventory/<repositoryId>`。仓库和权限策略只允许该身份写匹配 ID 的 `seed`/`last-known-good` owner，不能写其他引用或执行其他仓库操作。
4. Seed/LKG 可引用尚未复制进 Managed Repository 的对象。仓库仍验证快照结构、摘要、generation 和发布者绑定，但不要求对象已存在；GC 只会将其中实际存在于 Managed Catalog 的对象视为受保护。
5. LKG 必须是 Seed 精确集合的子集。当前部署工具把仓库服务、存储 Provider 和访问策略组成最小 LKG。未来自升级控制器必须先把候选复制进 Seed、原子写新 Inventory、重启并通过健康检查，最后才推进 LKG generation；失败回滚不得改写旧 generation。
6. 控制路径使用 Go：需要复用制品验签、配置 Codec、稳定 DTO、systemd/原子文件与集群寻址。Seed 服务不新增 Runtime 或进程。

## 故障语义

- Inventory 缺失、回退、LKG 非 Seed 子集、Seed 实物缺失/摘要不符或仓库发布失败均 fail-closed；
- Managed Repository 尚未启动时 Node Agent 保留运行实例但不报告完全收敛，下一轮以同一 Inventory generation/digest 重试；
- 仓库迁移镜像 Seed/LKG 快照；仓库重建后最多十分钟由 Inventory 心跳恢复；
- 插件级在线自升级已由 ADR-0102 补充：只有可信宿主可推进 LKG；内核二进制升级仍不得借此路径完成。

## 当前实施记录

- 2026-07-21（第一阶段）：完成 v1 清单契约、严格 YAML/JSON 读取、LKG 子集校验、Seed 实物逐项验签、受限集群发布、仓库缺对象兼容和开发环境 Inventory 生成。Node Agent ActualState v4 持久记录已发布 generation，Inventory 文件拒绝符号链接及 group/other 宽松权限；当时在线候选复制与健康后推进尚未实现。
- 2026-07-21（第二阶段）：按 [ADR-0102](ADR-0102-可信宿主仓库自升级事务.md) 完成插件级自升级控制器。候选复制、Inventory CAS、Runtime 健康、Assignment 保护和 LKG 推进已组成可恢复事务；内核二进制的 systemd 原子切换仍由独立运维流程负责。
