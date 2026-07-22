import { afterEach, describe, expect, it } from "vitest";
import { managementBinding, recordingPlatformInvoker, startPlatformManagementTestServer, type PlatformInvocation } from "../testing/platform-management-test-harness";

const close: (() => Promise<void>)[] = [];
afterEach(async () => Promise.all(close.splice(0).map((action) => action())));

describe("Platform deployment node routes", () => {
  it("maps node inventory and Bootstrap workflows with path-owned IDs", async () => {
    const calls: PlatformInvocation[] = [];
    const server = await start(calls, ["platform.deployment.read", "platform.deployment.write", "platform.deployment.bootstrap", "platform.deployment.approve"]);
    expect((await fetch(`${server.origin}/v1/portals/operations/platform/services/core/deployment/nodes`, { headers: server.readHeaders })).status).toBe(200);
    expect((await fetch(`${server.origin}/v1/portals/operations/platform/services/core/deployment/bootstrap-jobs`, { headers: server.readHeaders })).status).toBe(200);
    expect((await fetch(`${server.origin}/v1/portals/operations/platform/services/core/deployment/nodes/node-a`, { method: "PUT", headers: server.writeHeaders, body: '{"id":"forged","plan":{"node":{"id":"node-a","tenant":"tenant-a"}},"ifVersion":2}' })).status).toBe(200);
    expect((await fetch(`${server.origin}/v1/portals/operations/platform/services/core/deployment/nodes/node-a/bootstrap`, { method: "POST", headers: server.writeHeaders, body: "{}" })).status).toBe(200);
    expect((await fetch(`${server.origin}/v1/portals/operations/platform/services/core/deployment/bootstrap-jobs/job-1/approve`, { method: "POST", headers: server.writeHeaders, body: "{}" })).status).toBe(200);
    expect(calls.map(({ operation, payload }) => ({ operation, payload }))).toEqual([
      { operation: "listNodes", payload: {} }, { operation: "listBootstrapJobs", payload: {} },
      { operation: "putNode", payload: { id: "node-a", plan: { node: { id: "node-a", tenant: "tenant-a" } }, ifVersion: 2 } },
      { operation: "createBootstrap", payload: { nodeId: "node-a" } }, { operation: "approveBootstrap", payload: { jobId: "job-1" } },
    ]);
  });

  it("requires exact empty action bodies", async () => {
    const calls: PlatformInvocation[] = [];
    const server = await start(calls, ["platform.deployment.bootstrap"]);
    const invalid = await fetch(`${server.origin}/v1/portals/operations/platform/services/core/deployment/nodes/node-a/bootstrap`, { method: "POST", headers: server.writeHeaders, body: '{"force":true}' });
    expect(invalid.status).toBe(400);
    expect(calls).toEqual([]);
  });
});

async function start(calls: PlatformInvocation[], roles: string[]) {
  const operations = ["listNodes", "putNode", "listBootstrapJobs", "createBootstrap", "approveBootstrap"];
  const server = await startPlatformManagementTestServer(recordingPlatformInvoker(calls, (_capability, operation) => operation.startsWith("list") ? { items: [] } : {}), roles, managementBinding([{ capability: "platform.deployment", read: ["listNodes", "listBootstrapJobs"], write: operations.filter((item) => !item.startsWith("list")) }]));
  close.push(server.close);
  return server;
}
