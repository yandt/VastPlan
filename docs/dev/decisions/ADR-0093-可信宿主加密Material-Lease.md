# ADR-0093 可信宿主加密 Material Lease

- 状态：已采纳
- 日期：2026-07-20

## 背景

托管凭证已经由业务插件拥有生命周期、凭证插件保存 Vault Transit 密文，并以 `ManagedCredentialRef` 绑定 tenant、owner、purpose 和 version。真正执行数据库连接、制品存储或外部 API 调用的可信宿主仍需要在极短时间内使用明文，但凭证插件与执行宿主可能位于不同 Backend 服务甚至不同节点。

直接在 capability 响应中返回明文会使 NATS、协议日志、追踪器和中间转发层都进入秘密信任边界；让每个内核直接读取凭证插件状态或持有 Vault token，则会破坏插件的状态所有权和部署可替换性。

## 决策

采用由可信宿主发起、绑定调用身份的一次性加密 Material Lease：

1. Backend Kernel 的 `CredentialBroker` 为每次使用生成一次性 X25519 密钥对，只发送公钥与完整 `ManagedCredentialRef`。
2. `platform.credentials.material-lease/issue` 只接受宿主认证后的 `SYSTEM` caller。访问策略对普通插件和用户一律拒绝；源 Host 仍先执行本地权限强制点。
3. 凭证插件按 `CallContext.tenant_id` 查找完全匹配且处于 `Active` 的记录，通过 Vault Transit 解密，并在返回前再次检查记录未被退役、撤销或替换。
4. 凭证插件使用临时 X25519 发送方密钥协商共享秘密，经 HKDF-SHA256 派生 AES-256-GCM 密钥。租约密文的 AAD 固定绑定协议版本、lease ID、tenant、宿主 audience、完整 CredentialRef、签发时间和过期时间。信封的来源真实性由本地 capability 注册或签名 addressing 响应提供，生产环境不得绕过 transport trust。
5. 默认 TTL 为 15 秒，协议上限为 30 秒；接收端校验 claims、时间窗和 GCM 完整性后只能消费一次。明文仅传给内核可信适配器的同步回调，回调结束立即尽力清零。
6. 生产跨节点调用使用 transport trust 身份名作为 audience。该身份必须显式获得 `platform.credentials.material-lease` capability 和目标 `platform.credentials` logical service；本地不安全开发模式才使用 node ID。
7. Vault 工作负载 token 仍是自举根凭证，只能由 systemd credential 或受控只读挂载提供，不能由本机制反向托管。

该协议只负责把 material 安全送达可信宿主，不把解密权交给业务插件。数据库、HTTP、SSH 等具体 Broker 必须在宿主中完成操作，只向插件返回非敏感结果。

## 备选方案

- **凭证 capability 直接返回明文**：实现最简单，但扩大总线、日志和插件的泄漏面，否决。
- **所有 Backend Kernel 直接读取凭证状态并调用 Vault**：减少一次 RPC，但复制状态格式和 Vault 权限，破坏凭证插件所有权，否决。
- **长期节点公钥加密**：减少密钥生成成本，但泄漏长期私钥可解开被记录的历史响应，也更容易重放，否决。
- **要求消费者与凭证插件同进程**：无法支持跨服务部署与独立扩缩容，否决。

## 影响

- 协议载荷、NATS 和普通插件始终看不到凭证明文，跨节点仍可使用托管凭证。
- 每次 material 使用增加一次 capability 调用、一次 Vault decrypt 和一次短期非对称协商；这是最小秘密暴露边界的明确成本。
- Go 无法保证编译器不会复制内存，也没有 `crypto/ecdh.PrivateKey` 的显式销毁 API；实现采用短生命周期、单次消费、引用释放和可变明文缓冲区清零，但不宣称形式化的内存擦除保证。
- 当前已完成通用加密 lease、凭证插件签发、宿主 `CredentialBroker` 解封和 Node Agent 注入。数据库等领域 Broker 仍需分别接入，插件本身不得直接调用 material lease。
