# ADR-0091 制品存储 Provider 供给边界

- 状态：已采纳
- 日期：2026-07-20

## 背景

制品仓库需要支持本地文件、S3、OCI 等存储路线。直接让仓库的 HTTPS 后台 goroutine 对每个对象调用另一个 Provider 插件存在两个问题：插件跨能力调用必须继承一次受信任调用的 delegation token，后台 goroutine 没有该身份；即使放宽身份，每个对象再经过协议总线也会增加复制、延迟和故障面。

把所有驱动静态写进仓库主插件又会扩大依赖和供应链范围，不利于独立升级与隔离。

## 决策

存储 Provider 是**供给与迁移阶段插件**，不是逐对象数据代理：

1. Provider 在受信任配置调用中执行 `probe/provision/describe`，后续增加 `migrate/release`；
2. 返回不含秘密的 `Volume`：不透明 handle、provider ID、volume ID、generation、访问模式；
3. `filesystem` 模式返回只供部署适配器使用的 mount path；S3/OCI 可通过受控挂载或节点本地 sidecar 返回 `local-endpoint`，不把云凭证明文交给仓库插件；
4. 部署适配器把供给结果投影为仓库进程可访问的本地数据面；对象读写不走插件协议总线；
5. Provider 切换仍遵循 ADR-0090 的 probe、迁移、校验、candidate 激活和旧路线观察窗口。

首个实现为 `cn.vastplan.platform.artifacts.storage.file`。它仅在预配置私有根目录下幂等创建 `0700` volume，拒绝相对路径、目录逃逸、符号链接和 group/other 可访问目录。开发环境由它在仓库启动前供给 `repository.primary`。在插件可下载前的 Seed 仓库阶段，`seed-artifact-server` 以同样的 owner-only 目录约束直接托管本地根目录；它不依赖尚未可用的插件。

## 备选方案

- **每个对象通过插件 RPC 读写**：身份委托不成立，且热路径成本高，拒绝。
- **仓库主插件静态链接所有 SDK**：依赖、漏洞面和升级耦合持续增长，拒绝。
- **把云密钥注入仓库进程**：使存储驱动隔离失效，拒绝；凭证留在 Provider/sidecar。
- **只支持 POSIX 挂载**：实现简单但限制 OCI/对象网关，拒绝；契约同时预留本地 endpoint 数据面。

## 影响

- 仓库热路径保持本地数据面性能，Provider 仍可插件化演进。
- 部署适配器需要理解 volume handle，并负责 mount/sidecar 生命周期。
- Portal 普通用户只能看到 Provider ID 和健康状态，不能读取 mount path、endpoint 或凭证。
- 当前已实现通用 DTO、本地文件 Provider、私有目录强制、File A/B 在线迁移控制器和开发环境启动组合；切换与回滚细节见 [ADR-0099](ADR-0099-File-Volume在线迁移与可回滚切换.md)，S3/OCI Provider 仍待实现。
