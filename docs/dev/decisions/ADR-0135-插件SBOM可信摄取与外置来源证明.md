# ADR-0135：插件 SBOM 可信摄取与外置来源证明

状态：Accepted（2026-07-24）

## 背景

Backend 内核发布已经为独立二进制生成 CycloneDX 与平台来源证明，但全栈插件制品此前只有包摘要、签名清单和发布者证明。仓库无法回答某个插件是否携带依赖清单，也不能在管理中心展示经包体复验的 SBOM 摘要。

把来源证明直接塞入插件 tar 会产生摘要循环：证明的 subject 应绑定最终 tar SHA，而证明文件本身又会改变 tar SHA。让仓库只相信上传者提交的一段 SBOM 状态同样不成立，因为状态没有被不可变包与发布者签名覆盖。

## 决策

1. 插件 SBOM 使用标准 CycloneDX JSON 1.5/1.6，固定放在 `supply-chain/sbom.cdx.json`。签名清单的 `supplyChain.sbom` 绑定格式、规范版本、路径和精确 SHA-256；整个 tar SHA 又由现有 Ed25519 发布者证明签署。
2. `pluginpackage -sbom` 负责验证 SBOM、校验 `metadata.component` 必须等于插件 ID/版本、注入固定路径并写入清单摘要。`-package` 复用 testing 候选时禁止重新注入 SBOM，保证 stable 与 testing 仍是同一包字节。
3. 内核内容验证边界对任意来源重复执行有界 CycloneDX 解析、路径/摘要/主体绑定校验。校验器限制单文档 16 MiB、组件 100000 个，仅接受 1.5/1.6；仓库插件不能降低该边界。
4. 仓库 Catalog 只索引签名清单中的 SBOM 摘要，不在启动恢复时扫描全部大包。读取供应链证据时，再通过签名仓库读取完整包、复验包和发布者证明、提取 SBOM，并返回组件数、serial number 与 `verified` 状态。
5. 仓库默认要求 `stable` channel 携带 SBOM；可通过 `supplyChain.requiredSBOMChannels` 显式调整，空数组表示关闭强制。`testing` 默认允许缺少 SBOM，便于早期开发，但没有 SBOM 的候选不能按默认策略晋级 stable。
6. 构建来源证明保持包外 sidecar attestation。未来摄取 SLSA/in-toto 时必须由独立证明签名、精确 subject digest 与可信 issuer/workflow policy 绑定；不得把自报的 Git URL、commit 或 builder 字段当成可信来源证明。本 ADR 只预留该边界，不定义自有证明格式。

## 取舍

包内 SBOM 不能证明构建系统身份，但能保证“依赖声明与已签名插件包不可分离”，并允许所有语言使用各自最成熟的生成工具。可信校验继续在 Go 内核/仓库边界执行，避免把大包交给额外 Node/Python 进程；Node、Python、Rust 等生成器只产生标准文档，不取得仓库信任权。

证据详情按需读取大包，因此不会拖慢 Catalog 启动，但首次打开某个制品的供应链证据会产生一次完整包校验开销。这是显式审计操作，且不会进入列表轮询路径。
