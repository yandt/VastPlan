import { afterEach, describe, expect, it } from "vitest";
import { managementBinding, recordingPlatformInvoker, startPlatformManagementTestServer, type PlatformInvocation } from "../testing/platform-management-test-harness";

const close: (() => Promise<void>)[] = [];
afterEach(async () => Promise.all(close.splice(0).map((action) => action())));

describe("Platform core management routes", () => {
  it("routes Settings, Credentials and Database through verified server-owned targets", async () => {
    const calls: PlatformInvocation[] = [];
    const server = await startPlatformManagementTestServer(recordingPlatformInvoker(calls, (capability, operation) => capability === "platform.settings" && operation === "list" ? { items: [] } : {}), ["platform.settings.read", "platform.settings.write", "platform.credentials.write", "platform.credentials.rotate", "platform.database.write", "platform.database.probe"], fullBinding());
    close.push(server.close);
    expect((await fetch(`${server.origin}/v1/portals/operations/platform/services/core/settings?prefix=ui.`, { headers: server.readHeaders })).status).toBe(200);
    expect((await fetch(`${server.origin}/v1/portals/operations/platform/services/core/settings/ui.theme`, { method: "PUT", headers: server.writeHeaders, body: '{"key":"forged","value":"dark","ifVersion":2}' })).status).toBe(200);
    expect((await fetch(`${server.origin}/v1/portals/operations/platform/services/core/settings/ui.theme?ifVersion=2`, { method: "DELETE", headers: server.writeHeaders })).status).toBe(200);
    expect((await fetch(`${server.origin}/v1/portals/operations/platform/services/core/credentials/vault.db`, { method: "PUT", headers: server.writeHeaders, body: '{"name":"forged","value":"secret"}' })).status).toBe(200);
    expect((await fetch(`${server.origin}/v1/portals/operations/platform/services/core/credentials/vault.db/rotate`, { method: "POST", headers: server.writeHeaders, body: "{}" })).status).toBe(200);
    expect((await fetch(`${server.origin}/v1/portals/operations/platform/services/core/database-connections/main`, { method: "PUT", headers: server.writeHeaders, body: '{"name":"forged","providerId":"postgres","endpoint":"db:5432","options":{}}' })).status).toBe(200);
    expect((await fetch(`${server.origin}/v1/portals/operations/platform/services/core/database-connections/main/probe`, { method: "POST", headers: server.writeHeaders, body: "{}" })).status).toBe(200);
    expect(calls.map(({ capability, operation, payload }) => ({ capability, operation, payload }))).toEqual([
      { capability: "platform.settings", operation: "list", payload: { prefix: "ui." } },
      { capability: "platform.settings", operation: "put", payload: { key: "ui.theme", value: "dark", ifVersion: 2 } },
      { capability: "platform.settings", operation: "delete", payload: { key: "ui.theme", ifVersion: 2 } },
      { capability: "platform.credentials", operation: "put", payload: { name: "vault.db", value: "secret" } },
      { capability: "platform.credentials", operation: "rotate", payload: { name: "vault.db" } },
      { capability: "platform.database", operation: "define", payload: { name: "main", providerId: "postgres", endpoint: "db:5432", options: {} } },
      { capability: "platform.database", operation: "probe", payload: { name: "main" } },
    ]);
    expect(calls.every((call) => call.logicalService === "platform.core.primary")).toBe(true);
  });

  it("enforces Binding grants before roles", async () => {
    const calls: PlatformInvocation[] = [];
    const server = await startPlatformManagementTestServer(recordingPlatformInvoker(calls), ["platform.settings.write"], managementBinding([{ capability: "platform.settings", read: ["list"] }]));
    close.push(server.close);
    const denied = await fetch(`${server.origin}/v1/portals/operations/platform/services/core/settings/ui.theme`, { method: "PUT", headers: server.writeHeaders, body: '{"value":"dark"}' });
    expect(denied.status).toBe(403);
    expect(await denied.json()).toEqual({ error: "management_binding_forbidden" });
    expect(calls).toEqual([]);
  });
});

function fullBinding(): Record<string, unknown> {
  return managementBinding([
    { capability: "platform.settings", read: ["list"], write: ["put", "delete"] },
    { capability: "platform.credentials", read: ["list"], write: ["put", "rotate", "revoke"] },
    { capability: "platform.database", read: ["list"], write: ["define", "remove", "probe"] },
  ]);
}
