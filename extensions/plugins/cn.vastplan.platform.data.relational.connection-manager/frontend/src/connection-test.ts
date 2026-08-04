import { PlatformAdminError, type DatabaseProbe } from "@vastplan/platform-admin";
import { message, type CollectionActionResult } from "@vastplan/workbench-sdk";

const namespace = "cn.vastplan.platform.data.relational.connection-manager";

const diagnosticMessages: Readonly<Record<string, Readonly<{ key: string; fallback: string }>>> = {
  database_connection_invalid: { key: "test.invalidDetail", fallback: "连接参数无效，请检查地址、端口、用户名和传输加密设置。" },
  database_tls_policy_forbidden: { key: "test.tlsPolicyDetail", fallback: "当前部署策略不允许关闭传输加密校验，请启用完整校验或联系平台管理员。" },
  database_name_resolution_failed: { key: "test.dnsDetail", fallback: "数据库地址无法解析，请检查主机名和 DNS 配置。" },
  database_connection_refused: { key: "test.refusedDetail", fallback: "数据库服务器拒绝了连接，请检查地址、端口和服务监听状态。" },
  database_connection_timeout: { key: "test.timeoutDetail", fallback: "连接数据库超时，请检查网络、防火墙和连接超时设置。" },
  database_tls_verification_failed: { key: "test.tlsDetail", fallback: "传输加密或证书校验失败，请检查证书信任链和证书校验服务器名称。" },
  database_authentication_failed: { key: "test.authenticationDetail", fallback: "用户名或密码验证失败，请检查数据库账户信息。" },
  database_not_found: { key: "test.databaseNotFoundDetail", fallback: "指定的数据库不存在，请检查数据库名称。" },
  database_permission_denied: { key: "test.permissionDetail", fallback: "数据库账户没有连接或访问该数据库所需的权限。" },
  database_pool_exhausted: { key: "test.poolDetail", fallback: "数据库连接资源暂时不足，请稍后重试或检查连接数限制。" },
  database_runtime_unavailable: { key: "test.runtimeDetail", fallback: "数据库运行服务暂时不可用，请稍后重试。" },
};

export function connectionTestResult(result: DatabaseProbe): NonNullable<CollectionActionResult["notify"]> {
  if (!result.ready) return connectionTestFailure();
  return {
    title: message(namespace, "test.success", "连接测试成功"),
    content: message(namespace, "test.successDetail", "{provider} 响应正常，耗时 {latency} 毫秒", {
      provider: providerLabel(result.providerId),
      latency: Math.max(0, Math.round(result.latencyMs)),
    }),
    kind: "success",
  };
}

export function connectionTestFailure(error?: unknown): NonNullable<CollectionActionResult["notify"]> {
  const diagnostic = error instanceof PlatformAdminError ? diagnosticMessages[error.code] : undefined;
  const detail = diagnostic === undefined
    ? message(namespace, "test.failureDetail", "请检查地址、数据库、用户名、密码和传输加密设置后重试。")
    : message(namespace, diagnostic.key, diagnostic.fallback);
  return {
    title: message(namespace, "test.failure", "连接测试失败"),
    content: detail,
    kind: "error",
  };
}

function providerLabel(providerID: string): string {
  if (providerID === "postgresql") return "PostgreSQL";
  if (providerID === "mysql") return "MySQL";
  return providerID;
}
