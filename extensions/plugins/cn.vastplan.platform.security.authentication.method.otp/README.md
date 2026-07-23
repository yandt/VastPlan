# Enterprise One-Time-Code Authentication Provider

该插件实现 `authentication.method.v1` 的邮箱和短信一次性验证码方法，但不绑定任何短信、邮件厂商，也不保存普通用户。

- `enterprise-email-code` 与 `enterprise-sms-code` 只是可选 Method；只有管理员发布并绑定对应 Provider Profile 后才会出现在登录页。
- 投递统一调用 `foundation.security.authentication.delivery` 窄端口。具体 Delivery 服务负责解析企业标识、发送验证码并返回稳定主体；可由 Node、Java、Go 或供应商最成熟的语言实现。
- 验证码仅以内存 HMAC 保存，默认五分钟过期、六位数字、五次尝试、六十秒重发冷却；重发立即替换旧验证码，成功后通过 CAS 单次消费。
- 不存在的账号与可投递账号获得同形浏览器步骤。Delivery 返回未接受时，即使猜中随机码也不会产生 Authentication Evidence。
- 插件采用 `leader + leader-owned`，同一服务的所有请求路由到唯一所有者；进程故障会让未完成挑战失效并要求重新开始，不会放宽验证。
- `0.2.0` 实现语言中立 `configuration.v1` Service Hot 控制器。新配置先耐久 prepare，经独立审批后原子 commit；重复调用和进程重启按 candidate/request digest 恢复。
- Active 配置采用 revision/digest CAS，并写入插件私有 `0600` 状态文件。在途挑战固定创建时的 Profile，热切换不会改变已发送验证码的 issuer、TTL、重发或尝试策略；切换后的新挑战使用新 generation。

配置只保存非敏感策略和 Delivery Profile ID。`stateFile` 与内存 `capacity` 是部署所有的运行参数，不能通过热配置更改。短信/邮件供应商凭证由对应 Delivery 插件通过 Credential Material Lease 获取，不得放入本插件配置。控制器设计见 [ADR-0117](../../../docs/dev/decisions/ADR-0117-语言中立Service-Hot配置控制器.md)。
