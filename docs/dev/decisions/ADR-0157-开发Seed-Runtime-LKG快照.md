# ADR-0157 开发 Seed Runtime 的 Last-Known-Good 快照

- 状态：已采纳并实施
- 日期：2026-07-26
- 关联：[ADR-0101](ADR-0101-离线Bootstrap-Inventory与LKG推进.md)、[ADR-0142](ADR-0142-内核启动与业务发布完全分离.md)、[ADR-0146](ADR-0146-开发构建锁一致性与稳定制品身份账本.md)

## 背景

`platform-dev.sh up` 已不发布 Deployment 或 Portal Activation，但开发编排器仍在每次进程启动前根据当前工作区重建最小 Seed 插件仓库。只要共享 SDK、SBOM 或插件源码已经改变而消费插件尚未提升 SemVer，普通重启就会触发 stable 身份账本并失败。该行为实质上把“从源码生产候选制品”留在了启动路径，也使一个已验证可运行的平台无法仅因未完成的下一版源码而重启。

Bootstrap Inventory 中的 LKG 是托管仓库自举引用的业务契约，不保存 Backend Kernel、dynamic-go Host、Portal 静态宿主等本地开发运行材料，因此不能直接承担完整启动快照。

## 决策

1. 本地开发编排器增加独立的 `Seed Runtime Snapshot v1`。快照包含 Backend Kernel 与 dynamic-go Host、Portal 静态产物、未签名 Seed 包体/元数据、Seed Inventory、Access Profile Catalog 和 Backend Platform Catalog；不包含私钥、令牌、TLS、签名证明、进程状态或在线业务数据。
2. `bootstrap` 根据当前源码构建候选并执行 stable 身份校验，但只暂存快照。只有 Backend/Portal 最小宿主、显式平台发布与开发网关全部成功收敛后，才以内容摘要目录和原子活动指针提交为新的 Last-Known-Good；失败候选不得替换活动指针。
3. `up/restart` 优先恢复活动快照，禁止进入插件构建、打包和 stable 身份账本。包体在快照中保持未签名状态；每次运行重新生成短时本地 Seed 身份，对相同不可变包体重新签名并重新生成 Inventory，旧签名和私钥都不得进入快照。
4. 现有工作区第一次升级时，如果尚无活动快照，只允许迁移持久 `actual-state.json` 真正引用且结构完整的历史运行。不得按目录时间选择最近的失败构建。迁移后的快照立即完成摘要验证并原子提交；没有可信历史运行时，普通 `up` 可执行一次安全初始化构建，但仍不获得任何控制面业务发布权，并只在最小宿主成功后提交。
5. 快照 schema 也是宿主运行契约版本。编排器在支持某个 schema 期间必须保留该代插件/Assignment 所需的非秘密环境键或提供明确适配；不允许恢复旧包体后只注入新一代宿主键。当前 v1 同时提供旧 `POLICY_STATE` 与新 `POLICY_BOOTSTRAP_STATE` 的同路径别名，实际 Assignment allowlist 只会把其一交给对应插件。
6. 快照目录和活动指针保存在 `.vastplan/dev-platform/state/`，不进入 Git。活动指针、完成标记和 Inventory 必须是属主私有普通文件；快照拒绝符号链接、特殊文件、额外仓库文件、包体摘要不符、Inventory 之外的 ref 和内容摘要漂移，任何损坏均 fail-closed。
7. stable 身份账本仍是 `bootstrap`/候选生产路径的强制门禁，不能删除或改写来绕过 SemVer。一次构建存在多个漂移时必须聚合报告全部精确 ref，避免逐个修改、逐次失败。
8. 本 ADR 只规定本地开发编排器的运行材料。生产 Kernel/systemd 发布、Seed Bundle、仓库插件 LKG 和在线 Deployment 各自继续使用既有签名发布事务，不能复制本地快照机制作为生产升级旁路。

## 影响

普通启动恢复的是最近一次已证明可启动的完整 Seed Runtime，不再受工作区中未完成的下一版插件影响；源码构建、SemVer 校验与进程恢复成为两个可独立诊断的阶段。代价是本地状态新增一份内容寻址快照，并且修改 Seed 插件后必须显式执行 `bootstrap` 才会进入运行环境。`clean/--fresh` 删除开发运行状态后，首次启动需要重新建立快照，但 stable 身份账本仍按 ADR-0146 保留。
