import { afterEach, describe, expect, it } from "vitest";
import { CapabilityApplicationError, type TrustedCapabilityInvoker } from "../capabilities/capability-invoker";
import { managementBinding, recordingPlatformInvoker, startPlatformManagementTestServer, type PlatformInvocation } from "../testing/platform-management-test-harness";

const close: (() => Promise<void>)[] = [];
afterEach(async () => Promise.all(close.splice(0).map((action) => action())));

describe("Platform core management routes", () => {
  it("routes Platform Control bootstrap status, test and configure through the database capability", async () => {
    const calls: PlatformInvocation[] = [];
    const server = await startPlatformManagementTestServer(
      recordingPlatformInvoker(calls, (_capability, operation) => ({ phase: operation === "platformControlConfigure" ? "ready" : "unconfigured", generation: operation === "platformControlConfigure" ? 1 : undefined })),
      ["platform.database.read", "platform.database.probe", "platform.database.write"],
      fullBinding(),
    );
    close.push(server.close);
    const change = { profile: { schemaVersion: 1, generation: 1, connection: connectionRequest().connection, schema: "platform", secretRef: { kind: "systemd-credential", name: "platform-db" }, contractRange: "^1.0.0" }, expectedGeneration: 0 };
    expect((await fetch(`${server.origin}/v1/portals/operations/platform/services/core/platform-control`, { headers: server.readHeaders })).status).toBe(200);
    expect((await fetch(`${server.origin}/v1/portals/operations/platform/services/core/platform-control/test`, { method: "POST", headers: server.writeHeaders, body: JSON.stringify(change) })).status).toBe(200);
    expect((await fetch(`${server.origin}/v1/portals/operations/platform/services/core/platform-control`, { method: "PUT", headers: server.writeHeaders, body: JSON.stringify(change) })).status).toBe(200);
    expect(calls.map(({ capability, operation, payload }) => ({ capability, operation, payload }))).toEqual([
      { capability: "platform.database", operation: "platformControlStatus", payload: {} },
      { capability: "platform.database", operation: "platformControlTest", payload: change },
      { capability: "platform.database", operation: "platformControlConfigure", payload: change },
    ]);
  });

  it("routes Settings, Credentials and Database through verified server-owned targets", async () => {
    const calls: PlatformInvocation[] = [];
    const server = await startPlatformManagementTestServer(recordingPlatformInvoker(calls, (capability, operation) => capability === "platform.settings" && operation === "list" ? { items: [] } : {}), ["platform.settings.read", "platform.settings.write", "platform.credentials.audit", "platform.credentials.write", "platform.credentials.rotate", "platform.database.write", "platform.database.probe"], fullBinding());
    close.push(server.close);
    expect((await fetch(`${server.origin}/v1/portals/operations/platform/services/core/settings?prefix=ui.`, { headers: server.readHeaders })).status).toBe(200);
    expect((await fetch(`${server.origin}/v1/portals/operations/platform/services/core/settings/ui.theme`, { method: "PUT", headers: server.writeHeaders, body: '{"key":"forged","value":"dark","ifVersion":2}' })).status).toBe(200);
    expect((await fetch(`${server.origin}/v1/portals/operations/platform/services/core/settings/ui.theme?ifVersion=2`, { method: "DELETE", headers: server.writeHeaders })).status).toBe(200);
    expect((await fetch(`${server.origin}/v1/portals/operations/platform/services/core/credentials/vault.db`, { method: "PUT", headers: server.writeHeaders, body: '{"name":"forged","value":"secret"}' })).status).toBe(200);
    expect((await fetch(`${server.origin}/v1/portals/operations/platform/services/core/credentials/vault.db/rotate`, { method: "POST", headers: server.writeHeaders, body: "{}" })).status).toBe(200);
    expect((await fetch(`${server.origin}/v1/portals/operations/platform/services/core/credentials/managed-audit?beforeId=9&limit=50`, { headers: server.readHeaders })).status).toBe(200);
    expect((await fetch(`${server.origin}/v1/portals/operations/platform/services/core/database-connections/main`, { method: "PUT", headers: server.writeHeaders, body: JSON.stringify({ name: "forged", ...connectionRequest() }) })).status).toBe(200);
    expect((await fetch(`${server.origin}/v1/portals/operations/platform/services/core/database-connections/main/probe`, { method: "POST", headers: server.writeHeaders, body: "{}" })).status).toBe(200);
    expect((await fetch(`${server.origin}/v1/portals/operations/platform/services/core/database-connections/main/test`, { method: "POST", headers: server.writeHeaders, body: JSON.stringify(connectionRequest("temporary")) })).status).toBe(200);
    expect(calls.map(({ capability, operation, payload }) => ({ capability, operation, payload }))).toEqual([
      { capability: "platform.settings", operation: "list", payload: { prefix: "ui." } },
      { capability: "platform.settings", operation: "put", payload: { key: "ui.theme", value: "dark", ifVersion: 2 } },
      { capability: "platform.settings", operation: "delete", payload: { key: "ui.theme", ifVersion: 2 } },
      { capability: "platform.credentials", operation: "put", payload: { name: "vault.db", value: "secret" } },
      { capability: "platform.credentials", operation: "rotate", payload: { name: "vault.db" } },
      { capability: "platform.credentials", operation: "listManagedAudit", payload: { beforeId: 9, limit: 50 } },
      { capability: "platform.database", operation: "define", payload: { name: "main", ...connectionRequest() } },
      { capability: "platform.database", operation: "probe", payload: { name: "main" } },
      { capability: "platform.database", operation: "test", payload: { name: "main", ...connectionRequest("temporary") } },
    ]);
    expect(calls.every((call) => call.logicalService === "platform.core.primary")).toBe(true);
  });

  it("rejects malformed managed credential audit cursors before capability invocation", async () => {
    const calls: PlatformInvocation[] = [];
    const server = await startPlatformManagementTestServer(recordingPlatformInvoker(calls), ["platform.credentials.audit"], fullBinding());
    close.push(server.close);
    expect((await fetch(`${server.origin}/v1/portals/operations/platform/services/core/credentials/managed-audit?limit=999`, { headers: server.readHeaders })).status).toBe(400);
    expect(calls).toEqual([]);
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

  it("maps database connection-test failures without exposing Runtime detail", async () => {
    const invoker: TrustedCapabilityInvoker = { async invoke() { throw new CapabilityApplicationError("platform.database.connection_unavailable", "endpoint=db.internal password=do-not-leak"); } };
    const server = await startPlatformManagementTestServer(invoker, ["platform.database.probe"], fullBinding());
    close.push(server.close);
    const response = await fetch(`${server.origin}/v1/portals/operations/platform/services/core/database-connections/main/test`, { method: "POST", headers: server.writeHeaders, body: JSON.stringify(connectionRequest("temporary")) });
    expect(response.status).toBe(422);
    expect(await response.json()).toEqual({ error: "database_connection_failed" });
  });

  it("returns a database-specific safe code when the Provider rejects form parameters", async () => {
    const invoker: TrustedCapabilityInvoker = { async invoke() { throw new CapabilityApplicationError("platform.database.invalid", "provider-private-detail"); } };
    const server = await startPlatformManagementTestServer(invoker, ["platform.database.probe"], fullBinding());
    close.push(server.close);
    const response = await fetch(`${server.origin}/v1/portals/operations/platform/services/core/database-connections/main/test`, { method: "POST", headers: server.writeHeaders, body: JSON.stringify(connectionRequest("temporary")) });
    expect(response.status).toBe(422);
    expect(await response.json()).toEqual({ error: "database_connection_invalid" });
  });

  it("returns only whitelisted field-level validation details", async () => {
    const invoker: TrustedCapabilityInvoker = { async invoke() {
      throw new CapabilityApplicationError("platform.database.platform_control_invalid", "endpoint=db.internal password=do-not-leak", {
        validationField: "profile.connection.endpoint", validationReason: "host_port_required", internalPath: "/secret/path",
      });
    } };
    const server = await startPlatformManagementTestServer(invoker, ["platform.database.probe"], fullBinding());
    close.push(server.close);
    const response = await fetch(`${server.origin}/v1/portals/operations/platform/services/core/platform-control/test`, { method: "POST", headers: server.writeHeaders, body: "{}" });
    expect(response.status).toBe(422);
    expect(await response.json()).toEqual({ error: "platform_control_invalid", validation: { field: "profile.connection.endpoint", reason: "host_port_required" } });
  });

  it("preserves safe database diagnoses without exposing provider detail", async () => {
    const diagnoses = [
      ["platform.database.name_resolution_failed", "database_name_resolution_failed", 422],
      ["platform.database.connection_refused", "database_connection_refused", 422],
      ["platform.database.connection_timeout", "database_connection_timeout", 422],
      ["platform.database.tls_verification_failed", "database_tls_verification_failed", 422],
      ["platform.database.authentication_failed", "database_authentication_failed", 422],
      ["platform.database.database_not_found", "database_not_found", 422],
      ["platform_control.provisioning_failed", "platform_control_provisioning_failed", 422],
      ["platform.database.permission_denied", "database_permission_denied", 422],
      ["platform.database.pool_exhausted", "database_pool_exhausted", 429],
      ["platform.database.credential_unavailable", "database_credential_unavailable", 422],
      ["platform.database.credential_service_unavailable", "database_credential_service_unavailable", 503],
    ] as const;
    for (const [platformCode, browserCode, status] of diagnoses) {
      const invoker: TrustedCapabilityInvoker = { async invoke() { throw new CapabilityApplicationError(platformCode, "endpoint=db.internal password=do-not-leak"); } };
      const server = await startPlatformManagementTestServer(invoker, ["platform.database.probe"], fullBinding());
      close.push(server.close);
      const response = await fetch(`${server.origin}/v1/portals/operations/platform/services/core/database-connections/main/test`, { method: "POST", headers: server.writeHeaders, body: JSON.stringify(connectionRequest("temporary")) });
      expect(response.status).toBe(status);
      expect(await response.json()).toEqual({ error: browserCode });
    }
  });

  it("maps saved probe credential failures through the same database diagnosis", async () => {
    const invoker: TrustedCapabilityInvoker = { async invoke() { throw new CapabilityApplicationError("platform.database.credential_unavailable", "vault-private-detail"); } };
    const server = await startPlatformManagementTestServer(invoker, ["platform.database.probe"], fullBinding());
    close.push(server.close);
    const response = await fetch(`${server.origin}/v1/portals/operations/platform/services/core/database-connections/main/probe`, { method: "POST", headers: server.writeHeaders, body: "{}" });
    expect(response.status).toBe(422);
    expect(await response.json()).toEqual({ error: "database_credential_unavailable" });
  });

  it("preserves Platform Control Runtime diagnosis and trace identity", async () => {
    const traceId = "b".repeat(32);
    const invoker: TrustedCapabilityInvoker = { async invoke() {
      throw new CapabilityApplicationError("database.runtime.authentication_failed", "driver detail must not cross the BFF", {}, traceId);
    } };
    const server = await startPlatformManagementTestServer(invoker, ["platform.database.probe"], fullBinding());
    close.push(server.close);
    const response = await fetch(`${server.origin}/v1/portals/operations/platform/services/core/platform-control/test`, { method: "POST", headers: server.writeHeaders, body: "{}" });
    expect(response.status).toBe(422);
    expect(await response.json()).toEqual({ error: "database_authentication_failed", traceId });
  });
});

function fullBinding(): Record<string, unknown> {
  return managementBinding([
    { capability: "platform.settings", read: ["list"], write: ["put", "delete"] },
    { capability: "platform.credentials", read: ["list", "listManagedAudit"], write: ["put", "rotate", "revoke"] },
    { capability: "platform.database", read: ["list", "platformControlStatus"], write: ["define", "remove", "probe", "test", "platformControlTest", "platformControlConfigure"] },
  ]);
}

function connectionRequest(credentialValue?: string): Record<string, unknown> {
  return {
    connection: {
      providerId: "postgresql", endpoint: "db:5432", database: "vastplan", options: { user: "app", tlsMode: "verify-full", serverName: "db" },
      pool: { minIdle: 0, maxIdle: 8, maxOpen: 32, maxLifetimeMs: 1800000, maxIdleTimeMs: 300000, acquireTimeoutMs: 5000, idlePoolTtlMs: 900000 },
    },
    ...(credentialValue === undefined ? {} : { credentialValue }),
  };
}
