import { afterEach, describe, expect, it } from "vitest";
import { managementBinding, recordingPlatformInvoker, startPlatformManagementTestServer, type PlatformInvocation } from "../testing/platform-management-test-harness";

const close: (() => Promise<void>)[] = [];
afterEach(async () => Promise.all(close.splice(0).map((action) => action())));

const capability = "foundation.security.authentication.providers";
const prefix = "foundation.security.authentication.providers";

describe("Authentication Provider management routes", () => {
  it("uses server-owned provider IDs and separate operation permissions", async () => {
    const calls: PlatformInvocation[] = [];
    const binding = managementBinding([{ capability, read: ["get"], write: ["createDraft", "validate", "recordTest", "approve", "publish", "retire"] }]);
    const server = await startPlatformManagementTestServer(recordingPlatformInvoker(calls), [`${prefix}.read`, `${prefix}.edit`, `${prefix}.test`, `${prefix}.approve`, `${prefix}.publish`], binding);
    close.push(server.close);
    const base = `${server.origin}/v1/portals/operations/platform/services/core/authentication-providers`;
    expect((await fetch(base, { headers: server.readHeaders })).status).toBe(200);
    expect((await fetch(`${base}/corporate-oidc/validate`, { method: "POST", headers: server.writeHeaders, body: '{"expectedGeneration":2,"providerId":"forged"}' })).status).toBe(200);
    const assertion = { assertion: { schemaVersion: "v1", keyId: "broker-1", signature: "signed" } };
    expect((await fetch(`${base}/corporate-oidc/test`, { method: "POST", headers: server.writeHeaders, body: JSON.stringify({ expectedGeneration: 3, ...assertion, actor: "forged" }) })).status).toBe(200);
    expect((await fetch(`${base}/publish`, { method: "POST", headers: server.writeHeaders, body: '{"expectedGeneration":5,"catalogId":"auth","catalogRevision":1,"bindings":[],"accessCatalog":{"version":1}}' })).status).toBe(200);
    expect(calls.map(({ operation, payload }) => ({ operation, payload }))).toEqual([
      { operation: "get", payload: {} },
      { operation: "validate", payload: { expectedGeneration: 2, providerId: "corporate-oidc" } },
      { operation: "recordTest", payload: { expectedGeneration: 3, ...assertion, actor: "forged", providerId: "corporate-oidc" } },
      { operation: "publish", payload: { expectedGeneration: 5, catalogId: "auth", catalogRevision: 1, bindings: [], accessCatalog: { version: 1 } } },
    ]);
    expect(calls.every((call) => call.capability === capability)).toBe(true);
  });
});
