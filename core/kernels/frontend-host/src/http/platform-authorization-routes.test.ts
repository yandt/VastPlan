import { afterEach, describe, expect, it } from "vitest";
import { managementBinding, recordingPlatformInvoker, startPlatformManagementTestServer, type PlatformInvocation } from "../testing/platform-management-test-harness";

const close: (() => Promise<void>)[] = [];
afterEach(async () => Promise.all(close.splice(0).map((action) => action())));

const read = ["get", "listAudit"];
const write = ["createRole", "updateRole", "submitRole", "approveRole", "publishRole", "retireRole", "createBinding", "updateBinding", "submitBinding", "approveBinding", "publishBinding", "retireBinding", "revoke", "publishSnapshot"];

describe("Platform authorization routes", () => {
  it("maps fixed role, binding, revocation and snapshot workflows", async () => {
    const calls: PlatformInvocation[] = [];
    const roles = ["platform.authorization.catalog", "platform.authorization.role", "platform.authorization.binding", "platform.authorization.approve", "platform.authorization.publish", "platform.authorization.revoke", "platform.authorization.audit"];
    const server = await startPlatformManagementTestServer(recordingPlatformInvoker(calls, (_capability, operation) => operation === "listAudit" ? [] : {}), roles, binding());
    close.push(server.close);
    const base = `${server.origin}/v1/portals/operations/platform/services/core/authorization`;
    expect((await fetch(base, { headers: server.readHeaders })).status).toBe(200);
    expect((await fetch(`${base}/audit`, { headers: server.readHeaders })).status).toBe(200);
    expect((await fetch(`${base}/roles`, { method: "POST", headers: server.writeHeaders, body: '{"expectedGeneration":1,"id":"reader"}' })).status).toBe(200);
    expect((await fetch(`${base}/roles/reader/1`, { method: "PUT", headers: server.writeHeaders, body: '{"expectedGeneration":2}' })).status).toBe(200);
    expect((await fetch(`${base}/bindings/alice/1`, { method: "PUT", headers: server.writeHeaders, body: '{"expectedGeneration":3}' })).status).toBe(200);
    expect((await fetch(`${base}/roles/reader/1/approve`, { method: "POST", headers: server.writeHeaders, body: '{"expectedGeneration":4}' })).status).toBe(200);
    expect((await fetch(`${base}/revocations`, { method: "POST", headers: server.writeHeaders, body: '{"expectedGeneration":5}' })).status).toBe(200);
    expect(calls.map((call) => call.operation)).toEqual(["get", "listAudit", "createRole", "updateRole", "updateBinding", "approveRole", "revoke"]);
    expect(calls.every((call) => call.capability === "platform.authorization")).toBe(true);
  });

  it("requires an exact permission rather than a universal administrator role", async () => {
    const calls: PlatformInvocation[] = [];
    const server = await startPlatformManagementTestServer(recordingPlatformInvoker(calls), ["platform.admin", "platform.authorization.catalog"], binding());
    close.push(server.close);
    const response = await fetch(`${server.origin}/v1/portals/operations/platform/services/core/authorization/roles`, { method: "POST", headers: server.writeHeaders, body: "{}" });
    expect(response.status).toBe(403);
    expect(calls).toEqual([]);
  });
});

function binding(): Record<string, unknown> {
  return managementBinding([{ capability: "platform.authorization", read, write }]);
}
