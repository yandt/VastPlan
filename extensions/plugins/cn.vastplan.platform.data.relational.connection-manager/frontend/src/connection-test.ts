import { PlatformAdminError, type DatabaseProbe } from "@vastplan/platform-admin";
import { message, type CollectionActionResult } from "@vastplan/workbench-sdk";

const namespace = "cn.vastplan.platform.data.relational.connection-manager";

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
  const detail = error instanceof PlatformAdminError && error.code === "database_connection_invalid"
    ? message(namespace, "test.invalidDetail", "连接参数无效，请检查地址、端口、用户名和传输加密设置。")
    : message(namespace, "test.failureDetail", "请检查地址、数据库、用户名、密码和传输加密设置后重试。");
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
