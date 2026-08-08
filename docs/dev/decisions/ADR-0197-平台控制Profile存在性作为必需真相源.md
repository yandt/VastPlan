# ADR-0197 平台控制 Profile 存在性作为 Provider 必需的真相源

- 状态：已接受
- 日期：2026-08-08
- 关联：[ADR-0194 平台控制数据库与声明式数据分层](ADR-0194-平台控制数据库与声明式数据分层.md)、[ADR-0123 插件共享状态与可信 Provider](ADR-0123-插件共享状态与可信Provider.md)、[平台控制数据库 Bootstrap](../architecture/平台控制数据库Bootstrap.md)

## 背景

`sharedstate.BindingStore` 用单调状态区分 `unconfigured` 与 `unavailable`。这个区分承载一条安全边界：Authentication Broker 只把 `unconfigured` 解释为「尚未建立平台控制库」并据此回退到只读 Seed Catalog 完成首次登录，其余情况全部 fail-closed。

审计发现该边界的实现依据错了一层。「Provider 必需」由内存布尔 `BindingStore.required` 承载，而它的置位条件是 `Controller.Start()` **成功解析** Profile：

```go
profile, err := c.profiles.Load(ctx)
if err != nil {
    c.setStatus(PhaseRecovery, 0, CodeProfileInvalid)
    return err          // RequireProvider() 在此之后，永不执行
}
```

于是任何让 `Load` 失败的原因都等价于宣称「从未配置」。`FileProfileStore.Load` 有四条 error 路径，其中两条在运维中很现实：备份恢复或容器卷挂载把 Profile 权限改成 `0644`（owner-only 校验失败）；新版内核写入含新字段的 Profile 后回滚到旧内核（JSON Schema 校验拒绝）。结果是一个已承载生产数据的平台重新开放 Seed 登录路径。

同一根因还生出第二个缺陷。`writeOwnerFile` 在 `os.Rename` 成功后仍可能因目录 fsync 失败而返回 error，`Configure` 把它当作提交失败处理：`completeCommit(false)` 撤销必需标记，`prepared.Rollback()` 删除 `final` 密码文件——而 Profile 已经落盘并引用着它。磁盘上留下 generation N+1 的 Profile 指向不存在的密码文件，进程状态却回落到未配置；重启后永久停在 `PhaseRecovery/CodeSecretUnavailable`，且因 `Status` 报的 generation 与磁盘不一致，后续 Configure 持续 CAS 冲突，只能手工编辑磁盘恢复。

两个缺陷都不是编码疏忽，而是同一个建模错误：把「平台是否已配置」这一**持久事实**寄存在「内容当前是否可解析」这一**易失判断**上。文档声明的「读取到既有 Profile ... 即被永久标记为必需」被实现窄化成了「成功解析既有 Profile」，且现有测试的 `rejectingProfileStore.Load` 返回 `(nil, nil)`，从未覆盖 `Load` 返回 error 的分支。

## 决策

**一、Provider 必需的真相源是 Profile 路径上的存在性，与内容有效性解耦。**

`ProfileStore` 增加 `Exists(context.Context) (bool, error)`。`Controller.Start()` 先探测存在性并据此置位必需标记，再读取内容：

- 存在 → 立即 `RequireProvider()`，随后任何内容级失败只影响 phase 与错误码，不影响 `unconfigured`/`unavailable` 的判定；
- 探测本身失败 → 同样 `RequireProvider()`。无法证明平台是全新的，就不得开放 Seed 回退；
- 探测为不存在但随后 `Load` 读到内容 → 幂等再置位一次，覆盖探测与读取之间新建 Profile 的竞态；
- 探测为存在但随后 `Load` 返回 `(nil, nil)` → 视为信任边界故障进入 recovery，不得当作全新平台。

**二、秘密回滚与必需撤销的判据是「是否越过 rename」，不是 `Commit` 是否返回 error。**

`writeOwnerFile` 在 rename 成功之后的失败以 `ErrCommittedButUnsynced` 包装返回。`Configure` 对该错误按已提交处理：不撤销必需标记、不回滚秘密、继续 Bind 并进入 Ready。fsync 未完成这一事实以稳定码 `platform_control.profile_unsynced` 挂在 Ready 状态上保持可观测，而不是把一次成功的提交报成失败。

## 理由

真相源选磁盘存在性而非内存布尔，是因为「永久」这个语义必须有一个能跨进程重启重复观测的载体。内存布尔在重启后必然归零，其重建又依赖 `Load` 成功——这使得声称永久的决定实际可被一次权限变更撤销。文件是否存在是单调的、可重复观测的，且与内容质量正交，恰好匹配「是否曾经配置过」这个问题。

安全方向上取 fail-closed：探测失败时宁可拒绝服务也不开放 Seed 回退。可用性代价是明确的——损坏的 Profile 会让平台停在 recovery 等待人工修复，而不是静默降级成一个看起来全新、实则持有生产数据的平台。后者是更坏的结果。

rename 边界的选择遵循「不可回退点只应有一个」。rename 成功即事实成立，此后的补充操作（目录 fsync）失败只降低崩溃场景下的持久性保证，不改变已发生的事实。把它报成失败会驱动调用方去撤销一个无法撤销的动作。

## 影响

- `ProfileStore` 接口新增 `Exists`，两个实现（`FileProfileStore`、测试替身）同步更新；契约是内核内部端口，不涉及 wire 契约或插件 SDK。
- 新增稳定码 `platform_control.profile_unsynced`。它出现在 Ready 状态上，表示配置成功但目录项未 fsync，属于告警而非故障。
- 新增回归测试 `controller_durability_test.go` 覆盖三条此前无测试的路径：权限放宽的已提交 Profile 必须 fail-closed、无法解析的已提交 Profile 必须 fail-closed、rename 后 fsync 失败必须按已提交处理且不删除密码文件。已通过反向验证：临时还原旧 `Start()` 实现后，前两个测试如期失败。
- 未改动 `BindingStore.required` 仍为进程内存字段这一事实。它现在是正确的：进程内不可逆，重启后由磁盘存在性重新确立。文档中「永久」的表述已按此澄清。
- Backend 组合根在 Platform Control 与 NATS 候选都已确定后，只写入一次 `Dependencies.SharedState`。SQL 模式选择 `BindingStore`，否则选择 NATS Store；不再由装配函数的调用顺序覆盖前一个 Provider。架构门禁对 Backend 组合根的全部 `Dependencies.<field>` 写点执行单一所有者检查。
- 本 ADR 不涉及审计发现的其余缺陷（`Snapshot()` 就绪信号不反映当前可用性、`coordinator.Run` 订阅关闭后无兜底重试、bootstrap 层单元换版的发布路径成环、远端候选连接池泄漏）。它们根因不同，需各自单独处理。
