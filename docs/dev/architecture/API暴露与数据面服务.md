# API 暴露与数据面服务

本文是插件 API 契约、公开地址、Gateway 和独立数据面服务的单一真相源。架构取舍见 [ADR-0110](../decisions/ADR-0110-治理式API-Exposure与独立数据面.md)。

## 1. 边界

插件提供可调用能力，不自行占用公网路径。平台把签名插件清单中的能力分成四层：

1. `apiContracts`：插件声明 HTTP/JSON 契约、请求/响应 Schema、错误映射以及最终 `tool.package` 目标；
2. `ApiImplementation`：插件运行时只注册已有的内部 capability/operation；
3. `ApiExposure`：平台管理员把已验证契约绑定到 tenant、Portal/Host、认证 Profile、权限、限流和逻辑服务；
4. `dataPlaneServices`：大对象、流式或专用协议服务通过短时 `EndpointLease` 接入，不伪装成普通 RPC。

`api.route`/`apiRoutes` 只保留 Backend v1 兼容校验。新插件不得使用它发布产品 API，Node API Gateway 也不消费该扩展点。

## 2. 公开地址

普通 HTTP/JSON API 的公开地址固定为：

```text
/api/r/{routeKey}/v{major}/{contractPath}
```

`routeKey` 是控制面使用 CSPRNG 生成的 96-bit 随机值，经无填充 Base32 小写编码为 20 字符。它不是人工别名，也不是插件 ID、插件 ID hash、capability、service 或 node 的派生值。生成后在 Exposure 生命周期内保持稳定；删除后进入 tombstone，不得重新分配。

不采用插件 ID hash 的原因：截断 hash 仍需全局冲突检测；增加位数会让 URL 逐渐接近内部实现标识；插件替换、拆分、合并或发布者迁移会导致地址变化；字典枚举仍可把公开地址反推到已知插件。随机 Route Key 把公开身份与实现身份彻底分离。

`ExposureCatalog` 是下发给 Gateway 的不可变自包含快照，每条记录同时包含 Exposure 和已验证的完整 Contract。Contract Reference 绑定插件 ID、制品 SHA-256、贡献 ID、契约 ID/版本和规范摘要，但这些只存在可信控制面，不出现在公开 URL 或错误响应中。

## 3. 插件清单

```json
{
  "contributes": {
    "backend": {
      "tools": [{
        "id": "platform.example.items",
        "service_role": "backend",
        "subcommands": [{ "name": "listItems", "description": "查询条目" }]
      }],
      "apiContracts": [{
        "id": "management-api",
        "service_role": "backend",
        "contractId": "platform.example.items.api",
        "contractVersion": "1.0.0",
        "protocol": "http-json",
        "routes": [{
          "id": "platform.example.items.list",
          "method": "POST",
          "path": "/items",
          "target": { "capability": "platform.example.items", "operation": "listItems" },
          "requestSchema": { "type": "object", "additionalProperties": false },
          "responseSchema": { "type": "object", "additionalProperties": false },
          "successStatus": 200
        }]
      }]
    }
  }
}
```

宿主必须验证每条 route 只指向同一签名清单声明、且 `service_role` 相同的 tool/subcommand。请求和响应 Schema 必须内联且有界，禁止外部 `$ref`，避免运行期网络取 Schema、SSRF 和契约漂移。`apiContracts` 与 `dataPlaneServices` 是控制面元数据，不直接注册为协议总线扩展点。

## 4. Gateway 强制顺序

Gateway 固定执行：规范化 Host 与路径 → 解析当前 Catalog generation → 建立可信身份 → 校验 tenant/Portal/认证 Profile → 权限与速率预检 → 请求大小与 JSON Schema 校验 → 生成受限 `GatewayInvocation` → 调用内部 capability → Backend 最终 PEP 再校验 → 映射允许公开的错误 → 响应大小与 Schema 校验。

客户端不能提交 plugin ID、目标 capability、逻辑服务、路由域、tenant 或可信身份字段。Gateway 不透出内部堆栈、节点、插件、capability、NATS subject 或 gRPC 地址。

## 5. 独立数据面（兼容方案 C）

需要大文件、Range、流式传输或专用协议的插件声明 `dataPlaneServices`。运行实例只能向可信控制面登记最长 5 分钟的 HTTPS `EndpointLease`，Lease 绑定 Exposure、实例、SPIFFE 身份和允许模式：

- `gateway-proxy`：入口全程代理；
- `ticket-redirect`：入口签发一次性、短时、资源与主体绑定的 Ticket，再跳转到数据面；
- `private-direct`：只允许受信服务网格直接访问，不形成公网路径。

Endpoint 不得携带 userinfo、query 或 fragment。Lease 到期、实例不健康、制品撤销或 Exposure 退役后必须停止路由。制品仓库的大对象下载优先使用 `ticket-redirect`，目录、治理和小载荷操作使用普通 Gateway Contract。

## 6. 发布与演进

Exposure 使用 `Draft → Validated → Approved → Published → Retired`。发布产生新 generation，并以 CAS 原子替换 Gateway Catalog；Gateway 保留最近可用 generation，错误快照不得覆盖当前版本。Route Key 只标识公开 Exposure，契约兼容性由 URL 中的 major 和 Contract semver 控制。实现插件升级、迁移或替换不改变 Route Key，只更新受信 Contract Reference 和目标绑定。
