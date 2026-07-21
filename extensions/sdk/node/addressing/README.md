# @vastplan/addressing-node

VastPlan Node 进程对现有 Addressing v1 的原生实现。它直接消费签名 Capability Directory，并通过 NATS + Protobuf 调用 Go/Node/Python 等异构服务，不增加第二套私有 REST 控制面。

职责按文件分离：

- `protocol-codec.ts`：加载 `contracts/proto` 的 Addressing/Contract v1 Wire。
- `transport-security.ts`：NKey 信封、信任文档、重放保护与 capability 可见性授权。
- `capability-directory.ts`：签名 KV 公告、健康/租约/路由域选择。
- `addressing-client.ts`：有界 unary request/reply、响应身份绑定、超时与取消传播。
- `node-addressing.ts`：NATS mTLS、目录和资源生命周期组合。

生产必须配置 NATS mTLS、权限为 `0600` 或更严格的独立 NKey seed，以及包含 Portal Host 精确 capability allowlist 和 `allowDelegation` 的信任文档。本地明文 NATS 只能显式启用，不能成为生产默认值。

常用验证命令：

```bash
pnpm --filter @vastplan/addressing-node typecheck
pnpm --filter @vastplan/addressing-node test
pnpm --filter @vastplan/addressing-node build
go test -tags=e2e ./engineering/e2e -run TestNodeAddressingInvokesSecureGoCapability
```

最后一项会启动内嵌 NATS，由 Node 客户端读取 Go 发布的签名 Capability Directory，发送 NKey 签名的 Protobuf 请求，并验证 Go 返回的签名响应。
