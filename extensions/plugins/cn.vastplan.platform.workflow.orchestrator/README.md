# Workflow Orchestrator

通用流程编排插件拥有不可变流程定义修订、服务功能点绑定、运行实例、人工任务、领域动作工作项和审计。领域正文、冻结候选与最终发布仍由功能插件拥有。

功能点与节点目录只能通过 `registerCatalog` 从完整且摘要有效的 `ContributionIndexSnapshot` 装配；该操作只接受 Kernel `SYSTEM` 调用。普通调用不能提交 capability、operation 或权限覆盖值。

耐久状态写入 `platform.workflow.record` Record Store 模型。每个定义、绑定、实例、任务和动作使用独立 CAS 记录；跨记录推进使用 Record Store `UnitOfWork`，动作使用稳定 `instance/node/attempt` 幂等键。

协议和演进边界见 [通用流程管理](../../../docs/dev/architecture/通用流程管理.md) 与 [ADR-0205](../../../docs/dev/decisions/ADR-0205-流程管理插件与领域动作边界.md)。
