# ADR-0099 File Volume 在线迁移与可回滚切换

- 状态：已采纳并实施
- 日期：2026-07-21
- 关联：[ADR-0090](ADR-0090-插件配置与托管凭证闭环.md)、[ADR-0091](ADR-0091-制品存储Provider供给边界.md)、[ADR-0098](ADR-0098-制品依赖解析锁与离线Bundle.md)

## 背景

仓库已把 Storage Provider 限制在供给与迁移控制路径，但仅有 `probe/provision`。直接修改仓库路径并重启无法保证复制期间的新发布、Catalog revision 和回滚数据一致；让 Provider 自行决定切换，又会越过仓库 leader 对发布可见性的唯一权威。

## 决策

1. 仓库 leader 内的迁移控制器独占状态机、发布冻结、Catalog 校验和活动数据面指针；File Provider 只执行 `describe/provision/migrate/release` 物理 volume 操作。二者使用 Go 并留在已有的两个可信 leader 进程，不新增运行时或守护进程。
2. 迁移不启动脱离请求身份的后台 goroutine。稳定命令为 `prepare -> sync -> cutover -> observing -> finalize -> release`，每步按同一 `migrationId` 幂等重试；超时只留下可恢复状态，不伪造 durable 用户身份。
3. `prepare` 只绑定一个空候选 volume。`sync` 只复制普通文件，拒绝符号链接和特殊文件，逐文件 SHA-256 校验并输出排序清单摘要；取消或崩溃后可重做，临时文件不会变成正式对象。
4. `cutover` 只在最终增量同步期间冻结发布；已有读取仍在不可变源卷上完成。候选卷必须以本地信任根重建 Catalog，且 revision、制品数与库存摘要全部相同后，才先持久化 `observing`、再原子切换。任一失败保留源卷活动。
5. 观察窗口内读取使用候选卷，发布按“旧卷镜像先写、新卷活动后写”双写。镜像失败时新卷不接受该发布并记录错误；活动新卷失败时允许回滚到包含更多已验签对象的旧卷，但禁止 finalize。
6. `finalize` 只在观察时间已满、无镜像错误且两份 Catalog 一致时停止双写。`release` 只在部署配置已指向目标 provider/volume 并重启确认后允许；File Provider 仅将旧卷原子移入私有 quarantine，不立即删除字节。
7. 物理 handle、mount path、endpoint 只保存在 `0600` 迁移状态中。Portal/BFF 只返回 provider ID、volume ID、阶段、统计、观察时间与可执行动作。Storage Provider capability 只允许精确仓库插件调用，用户和其他插件不能直达。

## 备选方案

- **Deployment Manager 统一控制**：有利于通用资源编排，但它不拥有仓库发布增量和 Catalog 可见性，会形成双控制器，拒绝。
- **Provider 自行切换**：物理复制直接，但 Provider 无权冻结发布或决定对外可见性，拒绝。
- **停机全量拷贝后改路径**：实现最少，但停机与回滚时间随仓库容量线性增长，不作为在线方案。
- **异步后台一键迁移**：产品上简单，但当前没有可持久委托的管理命令身份；不得持久化用户 `CallContext`，因此采用可重试阶段命令。

## 影响

- 正面：File A/B 切换的长时复制在线完成，只在最终追平时短暂冻结发布，读取不依赖 Provider RPC。
- 正面：每个崩溃窗口都有持久化先后顺序；重启能确定选择源卷或目标卷。
- 代价：观察窗口双写增加发布延迟和短期容量开销；任一卷异常都会冻结新发布。
- 边界：本版只支持同一 File Provider 下的 filesystem volume A/B。S3/OCI 与跨 Provider 数据泵必须实现相同分阶段契约，不能扩大仓库内核或暴露云凭证。

## 实施记录

- 2026-07-21：File Provider 0.2.0 完成安全目录清单、可取消原子复制、摘要校验与 quarantine release；仓库 0.6.0 完成持久状态、候选 Catalog 验证、发布短冻结、观察双写、回滚、配置门禁释放、Portal BFF 和 TypeScript SDK。
- 2026-07-21：隔离开发平台完成真实跨进程验收：`repository.primary` 经管理 BFF prepare/sync 后切换到 `repository.e2e`，状态与 Catalog 摘要保持一致，再从观察窗口回滚到 `repository.primary`。同时为仓库与 File Provider 增加运行时贡献描述符对签名 manifest 的精确一致性测试，避免安全宿主因描述漂移拒绝插件启动。
