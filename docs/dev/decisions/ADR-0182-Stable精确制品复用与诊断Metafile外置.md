# ADR-0182 Stable 精确制品复用与诊断 Metafile 外置

- 状态：已采纳
- 日期：2026-08-01
- 关联：[ADR-0146](ADR-0146-开发构建锁一致性与稳定制品身份账本.md)、[ADR-0157](ADR-0157-开发Seed-Runtime-LKG快照.md)、[ADR-0165](ADR-0165-Contract-Registry与插件发布编排.md)

## 背景

显式 `bootstrap --rebuild-seed` 曾从当前工作区重新构建 Profile 中全部 stable 插件，再用身份账本拒绝同一精确引用的不同字节。这能防止覆盖，但把 Seed 重建错误地等同于“重新发布所有依赖”。一次只修改 TypeScript 类型的公共 SDK 变更，就会改变多个插件的 esbuild metafile 输入字节数；少数 bundle 还会仅因压缩符号重新分配产生几个字节差异。结果是没有功能变化的旧 stable 版本阻断真正需要晋级的新插件。

esbuild 原始 metafile 是 SBOM 和 Module Graph 构建期证据，包含源码字节数和物理依赖路径。运行期只需要签名 Module Graph；把原始 metafile继续装入正式插件包，会让非运行事实参与制品身份。

## 决策

1. 本地 Seed 构建把 stable 精确引用视为仓库输入，而不是可从当前源码重复生产的标签。普通 stable 包在源码打包前直接从对象缓存装入候选仓库，只有未登记的新 SemVer 才从源码打包；dynamic-go 因还需匹配当前 Host ABI variant，在变体核验后复用。`.vastplan/stable-package-identities.json` 已登记的 `pluginId + version + channel + dynamic-go variant` 必须保留其原始包字节，未提升 SemVer 的工作区变化不进入 Seed。
2. 首次接受的新 stable 精确引用同时把未签名包保存到 `.vastplan/stable-packages/objects/<prefix>/<sha256>.tar.gz`。对象以 `0600` 创建，写入后同步目录；读取时复验 SHA-256、包内 Manifest、精确引用与 dynamic-go fingerprint。
3. 已有身份账本首次迁移时，可从受控开发构建缓存、Seed Runtime Snapshot 或历史运行仓库恢复相同 SHA-256 对象；恢复后立即写入独立对象缓存。记录对象缺失、损坏、身份不符或缓存冲突时 fail closed，不允许拿当前源码替代。
4. 新 SemVer 或新的 dynamic-go variant 仍从当前源码构建、验证并登记。`workspace/testing/dev.*` 继续走测试发布协议，不进入 stable 对象缓存。正式远端仓库仍是生产制品真相源，本地缓存只负责开发 Seed 的不可变复用。
5. esbuild 原始 `vastplan.*-metafile.json` 只用于构建期 SBOM、按需加载门禁和 Module Graph 生成；正式插件 staging 在打包前删除它。签名 Module Graph、运行节点、SBOM 和 Manifest 仍保留。即使调用方使用 source-only 打包路径，也不得绕过清理。
6. `--fresh` 可以清理运行态与普通构建缓存，但不能删除 stable 身份账本和对象缓存。工作区源码若要进入 stable，必须提升插件 SemVer 并同步所有精确引用；复用旧对象不能充当发布动作。

## 备选方案

- 给全部漂移插件机械提升 patch：可以启动，但会为类型字节数和压缩变量名制造无功能版本，且每次共享 SDK 变化都会重复发生，否决。
- 继续重建并只忽略 metafile：能减少一部分噪声，但仍把旧 stable 当成可重复发布目标，无法消除工具链和压缩器差异，否决。
- 允许账本覆盖：破坏回滚、缓存、签名和精确引用语义，否决。

## 影响

- Seed 重建只会从源码打包并晋级明确提升版本的插件；其余普通 stable 依赖与远端包管理器一样在打包前复用原字节。
- 本地首次迁移会读取一次历史可信对象并建立独立缓存，后续不依赖历史运行目录。
- 开发者修改旧版本源码后执行 Seed 重建时，平台会继续运行旧 stable，并明确提示建议的新版本；调试新代码应使用 workspace/testing。
- 原始 metafile 不再是正式制品内容，但自动 SBOM 仍在清理前消费它，供应链依赖证据不降低。
