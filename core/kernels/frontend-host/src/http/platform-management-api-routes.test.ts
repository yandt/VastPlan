import { afterEach, describe, expect, it } from "vitest";
import type { APIContractCatalogPort, APIContractContribution } from "../api-exposure/api-exposure-contract";
import { managementBinding, recordingPlatformInvoker, startPlatformManagementTestServer, type PlatformInvocation } from "../testing/platform-management-test-harness";

const close: (() => Promise<void>)[] = [];
afterEach(async () => Promise.all(close.splice(0).map((action) => action())));

describe("Contract-driven Platform Management API", () => {
  it("resolves an exact contract and never accepts a browser-selected capability", async () => {
    const calls: PlatformInvocation[] = [];
    const catalog: APIContractCatalogPort = { async resolveContract(reference) { return reference.contractDigest === "a".repeat(64) ? contract : undefined; } };
    const binding = managementBinding([{ capability: "platform.example", read: ["apiList"], write: ["apiCreate"] }]);
    (binding.services as Record<string, unknown>[])[0]!.apis = [{ id: "primary", contractId: contract.contractId, contractVersion: contract.contractVersion, contractDigest: "a".repeat(64) }];
    const server = await startPlatformManagementTestServer(recordingPlatformInvoker(calls, (_capability, operation) => operation === "apiList" ? { items: [] } : { id: 7 }), [], binding, undefined, catalog);
    close.push(server.close);
    const base = `${server.origin}/v1/portals/operations/platform/services/core/api/primary`;
    expect((await fetch(`${base}/items`, { headers: server.readHeaders })).status).toBe(200);
    expect((await fetch(`${base}/items`, { method: "POST", headers: server.writeHeaders, body: '{"name":"demo"}' })).status).toBe(201);
    expect((await fetch(`${base}/missing`, { headers: server.readHeaders })).status).toBe(404);
    expect(calls.map((call) => [call.capability, call.operation])).toEqual([["platform.example", "apiList"], ["platform.example", "apiCreate"]]);
    expect(calls[1]!.payload).toMatchObject({ schemaVersion: "v1", routeId: "platform.example.create", body: { name: "demo" } });
  });

  it("fails closed when the exact contract digest is unavailable", async () => {
    const binding = managementBinding([{ capability: "platform.example", read: ["apiList"] }]);
    (binding.services as Record<string, unknown>[])[0]!.apis = [{ id: "primary", contractId: contract.contractId, contractVersion: contract.contractVersion, contractDigest: "b".repeat(64) }];
    const catalog: APIContractCatalogPort = { async resolveContract() { return undefined; } };
    const server = await startPlatformManagementTestServer(recordingPlatformInvoker([]), [], binding, undefined, catalog);
    close.push(server.close);
    const response = await fetch(`${server.origin}/v1/portals/operations/platform/services/core/api/primary/items`, { headers: server.readHeaders });
    expect(response.status).toBe(409);
  });
});

const contract: APIContractContribution = {
  id: "management-api", service_role: "backend", contractId: "platform.example.management", contractVersion: "1.0.0", protocol: "http-json",
  routes: [
    { id: "platform.example.list", method: "GET", path: "/items", target: { capability: "platform.example", operation: "apiList" }, requestSchema: { type: "object", maxProperties: 0 }, responseSchema: { type: "object", required: ["items"], properties: { items: { type: "array" } } }, successStatus: 200 },
    { id: "platform.example.create", method: "POST", path: "/items", target: { capability: "platform.example", operation: "apiCreate" }, requestSchema: { type: "object", required: ["name"], properties: { name: { type: "string" } }, additionalProperties: false }, responseSchema: { type: "object", required: ["id"], properties: { id: { type: "integer" } } }, successStatus: 201 },
  ],
};
