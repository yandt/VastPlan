# ADR-0136：外部来源证明 Verifier 与双端复验

状态：Accepted（2026-07-24）

## 背景

ADR-0135 已把插件 CycloneDX SBOM 绑定进发布者签名覆盖的不可变包，但构建来源证明必须以最终 tar SHA-256 为 subject，因此不能再放回 tar 内。来源证明还存在多种信任体系：GitHub/Sigstore keyless、企业 CA、离线签名构建机以及未来其他 in-toto 实现。把所有证书链、透明日志和 OIDC 逻辑编译进仓库插件或 Backend 内核，会扩大信任基座和升级耦合。

仓库 HTTPS 上传发生在长期服务的后台路径，不携带可转授给普通插件的用户调用上下文。让仓库在上传时临时调用一个普通 Verifier 插件，会迫使系统伪造 delegation token 或保存用户 CallContext；只在 CI 客户端自报“验证成功”则没有可信意义。只在仓库准入时检查也不够：Node Agent 面对被入侵或错误配置的远端仓库时仍必须独立验证安装条件。

## 决策

1. 原始来源证明使用包外 DSSE envelope，payload 必须是 in-toto Statement v1，predicate 必须是 SLSA Provenance v1，且至少一个 subject 的 SHA-256 精确等于最终插件 tar SHA-256。
2. 外部 Verifier Provider 负责验证 DSSE 签名、证书/OIDC/透明日志或企业等价信任链，并签发规范化 `ProvenanceVerificationRecord v1`。记录绑定：原始证明 SHA-256、插件包 SHA-256、predicate type、builder ID、build type、源码 URI/commit、issuer、workflow、Provider/key/policy 身份和有效期。
3. Repository 与 Node Agent 不信任 Provider 的普通 RPC 返回。它们只接受由部署信任文档中 Verifier 公钥签署的 Verification Record，并独立执行：有界 JSON/base64 解析、记录签名、当前密钥状态、记录有效期、原始证明摘要、in-toto subject、规范化摘要字段和本地 publisher/channel/prefix 策略匹配。
4. 原始 DSSE 与 Verification Record 都是包外不可变 sidecar。远端上传、读取、离线 Bundle、存储迁移、GC 和恢复必须把二者视为同一制品 ref 的证据集合；不能只有 Catalog 布尔状态。缺一项、摘要漂移或同 ref 出现不同 sidecar 均 fail-closed。
5. testing → stable 审批除包 SHA、publisher 与发布 key 外，还绑定 testing 的原始 Provenance 和 Verification Record 摘要。stable 必须原样复用同一对 sidecar；若记录在审批后过期，该不可变 ref 不得续签或复活，必须发布新插件版本、重新验证并重新审批。
6. 来源证明要求由部署信任文档按 channel、publisher 和插件命名空间规则决定；精确规则优先。生产默认对 `stable` 要求来源证明，本地开发 Profile 可显式把强制 channel 设为空，但不能伪装成生产等价环境。
7. Provider 协议是扩展点，首个参考 Provider 使用 Go、运行在可信独立进程。Go 更适合有界解析、密码学、公钥信任和低资源常驻服务；Node 的 Sigstore 生态可作为后续 Provider，Python 适合策略分析但不作为首个强制点。运行语言与信任方式分离：任何语言的 Provider 都必须输出同一签名 Verification Record。
8. 仓库/Node Agent 的记录复验器保持为小型 Go 共享包，只实现稳定格式、签名和策略，不联网、不访问透明日志、不执行插件内容。外部生态升级只替换 Provider，不改变安装强制点。

## Provider 契约

Provider 输入只包含有界原始 DSSE、精确 subject SHA-256、待应用 policy ID 和必要的非秘密验证选项；不得接收制品执行权限、仓库写权限或发布令牌。输出记录不得包含证书私钥、OIDC token、仓库令牌或完整源码环境。

首个静态密钥 Provider 用于企业离线构建机和本地确定性测试：验证 DSSE PAE 上的 Ed25519 签名，再按 builder/build type/source 规则签发记录。GitHub/Sigstore Provider 后续可验证 Fulcio/Rekor/OIDC，并把已验证 issuer/workflow 投影进同一记录。Repository 不根据字段是否“看起来像 GitHub”自行推断信任。

## 取舍

双层签名增加一个 Verification Record 和两次本地复验，但避免内核绑定每种供应链生态，也避免长期仓库请求路径跨插件借权。记录有效期由企业按发布周期设置，默认一年、最长十年；到期或 key 撤销后以新插件版本重新构建、验证和发布。这是显式信任生命周期，不用签发者自报历史时间掩盖已撤销密钥。

Sidecar 不改变插件包字节，因此同一 testing/stable 包可以复用；但 sidecar 摘要必须进入审批和安装策略，不能把“包相同”误解为“来源证明可以任意替换”。
