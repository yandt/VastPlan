# ADR-0122 CredentialRef 与 Scoped 配置多语言 SDK

- 状态：已采纳
- 日期：2026-07-23

## 背景

`ManagedCredentialRef` 和 `configuration.scoped.v1` 已成为插件读取托管凭证引用与 Tenant/User Hot 配置的语言中立边界。只有 Go consumer 会导致 Node、Python 插件复制校验规则；而 Python 默认采用共享 `python-subinterpreter` Runtime，原桥接只支持 Invoke，无法在处理器内 HostCall，迫使配置消费者改用独立进程。

跨语言 SDK 不能扩大信任面：配置目标、tenant、subject、插件 ID 与 CredentialRef owner 仍必须由可信宿主从认证会话和 `CallContext` 推导，插件请求不得自报这些身份。SDK 也不能读取凭证明文；material 继续只经可信宿主签发的短租约进入获授权 Runtime。

## 决策

1. Go 契约校验是行为真源；`contracts/testdata/sdk-interop-v1.json` 固定 Go、Node、Python 共用的 CredentialRef 与规范 JSON 摘要向量。JSON Schema/Wire、golden 和跨语言测试共同阻止实现漂移。
2. `ManagedCredentialRef` 是闭合、不可变值对象。SDK 严格校验 handle、scope、owner、purpose、version 与可选 name，拒绝未知字段、明文和值外身份；业务代码不再自行复制 validator。
3. Go、Node、Python 均提供 `configuration.scoped.v1` consumer。`resolve` 请求固定为空对象，`watchRevision` 只携带 revision、digest 和有界 timeout；响应先做未知字段、状态、时间、Seed/Active 语义与规范摘要验证，再暴露不可变值。
4. Scoped 配置继续不承载 CredentialRef。含凭证的 Service Hot 配置仍走候选凭证、提交和退役状态机，不允许以普通 Tenant/User Scoped 值绕过。
5. Python 独立进程 SDK 接受 Protobuf 或纯映射 target/context；共享子解释器 SDK 只接受可跨解释器复制的纯映射。主解释器桥接在原始 Invoke 线程执行 HostCall，复用宿主保存的 opaque delegation token；token 不进入子解释器消息、`CallContext.metadata` 或业务可构造字段。
6. 子解释器 HostCall 受父调用 deadline、30 秒上限、4 MiB payload、严格 Protobuf 解析和取消约束。乱序消息、未知字段、桥关闭与超时均 fail-closed。事件发布、动态贡献和状态迁移仍未由该桥接承诺。
7. SDK 是随对应语言 Runtime 使用的库，不新增进程。Go 适合内核与原生插件；Node SDK 服务 Node Worker 生态；Python SDK 服务数据、AI 与自动化插件。其他语言后续按相同 Wire/Schema/golden 增加薄客户端，而不是复刻可信身份选择逻辑。

## 备选方案

### 只保留 Go SDK

实现最少，但会把安全关键校验分散到 Node/Python 业务插件，长期漂移风险最高，不采用。

### Python Scoped 消费者一律独立进程

能复用已有 gRPC HostCall，却破坏“同一内核服务、同语言插件默认共享 Runtime”的资源目标。配置读取本身不应成为专用进程理由，不采用。

### 把 delegation token 传入子解释器

实现直接，但把宿主签发的委托能力暴露给插件代码，增加复制和误用面，不采用。

### 为每种语言分别定义 CallContext/配置协议

短期更贴合语言习惯，长期会形成身份、摘要和版本语义不兼容，不采用。

## 影响

正面影响：三种主要插件语言获得一致、闭合的配置消费 API；Node 重复 CredentialRef 校验被集中；Python 共享 Runtime 能安全 HostCall；跨语言摘要可由同一向量回归。

代价：Python 主/子解释器桥需要维护有界的嵌套请求状态机；任何 Wire 变化都必须同时更新三种 SDK 和 golden；共享子解释器仍不是第三方安全沙箱，也没有开放全部 PluginHost feature。
