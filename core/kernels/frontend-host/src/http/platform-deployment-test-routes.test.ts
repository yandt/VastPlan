import { afterEach, describe, expect, it } from "vitest";
import { managementBinding, recordingPlatformInvoker, startPlatformManagementTestServer, type PlatformInvocation } from "../testing/platform-management-test-harness";

const close: (() => Promise<void>)[] = [];
afterEach(async () => Promise.all(close.splice(0).map((action) => action())));

describe("Platform deployment test release routes", () => {
  it("maps governed backend test target and release operations", async () => {
    const calls: PlatformInvocation[] = [];
    const read = ["listTestTargetBindings", "listTestReleases"];
    const write = ["putTestTargetBinding", "createTestRelease", "rollbackTestRelease"];
    const server = await startPlatformManagementTestServer(recordingPlatformInvoker(calls, (_capability, operation) => read.includes(operation) ? { items: [] } : {}), ["platform.admin"], managementBinding([{ capability: "platform.deployment", read, write }]));
    close.push(server.close);
    const base = `${server.origin}/v1/portals/operations/platform/services/core/deployment`;
    expect((await fetch(`${base}/test-target-bindings`, { headers: server.readHeaders })).status).toBe(200);
    expect((await fetch(`${base}/test-target-bindings/backend-a`, { method: "PUT", headers: server.writeHeaders, body: '{"id":"forged","pluginId":"cn.demo"}' })).status).toBe(200);
    expect((await fetch(`${base}/test-releases`, { headers: server.readHeaders })).status).toBe(200);
    expect((await fetch(`${base}/test-releases`, { method: "POST", headers: server.writeHeaders, body: '{"bindingId":"backend-a"}' })).status).toBe(200);
    expect((await fetch(`${base}/test-releases/8/rollback`, { method: "POST", headers: server.writeHeaders, body: "{}" })).status).toBe(200);
    expect(calls.map(({ operation, payload }) => ({ operation, payload }))).toEqual([
      { operation: "listTestTargetBindings", payload: {} },
      { operation: "putTestTargetBinding", payload: { id: "backend-a", binding: { id: "forged", pluginId: "cn.demo" } } },
      { operation: "listTestReleases", payload: {} },
      { operation: "createTestRelease", payload: { release: { bindingId: "backend-a" } } },
      { operation: "rollbackTestRelease", payload: { releaseId: 8 } },
    ]);
  });
});
