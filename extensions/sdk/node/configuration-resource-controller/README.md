# VastPlan Node Configuration Resource Controller SDK

`@vastplan/configuration-resource-controller-node` 将 Node 插件接入语言中立 `configuration.resource.v1`。它严格处理 `list/get/prepare/commit/abort/status`、派生不透明 capability 与 `cfgc_*`，并确保公开查询只返回非敏感 values 和凭证状态。

插件仍负责持久化 `cfgp_*` Active/Candidate、原子 CAS、幂等恢复、替换引用合并及旧引用退役。SDK 运行在已有共享 `node-worker` 中，不为每个控制器增加进程。完整边界见 [ADR-0118](../../../../docs/dev/decisions/ADR-0118-独立配置资源与动态Profile.md)。
