import { afterEach, describe, expect, it } from "vitest";
import { managementBinding, recordingPlatformInvoker, startPlatformManagementTestServer, type PlatformInvocation } from "../testing/platform-management-test-harness";

const close: (() => Promise<void>)[] = [];
afterEach(async () => Promise.all(close.splice(0).map((action) => action())));

describe("Seed handoff management routes", () => {
  it("keeps proof server-owned and rejects handoff from legacy file sessions", async () => {
    const calls: PlatformInvocation[] = [], capability = "foundation.security.seed.handoff";
    const binding = managementBinding([{ capability, read: ["get"], write: ["configureProvider", "verifyProvider", "prepareHandoff", "completeHandoff"] }]);
    const server = await startPlatformManagementTestServer(recordingPlatformInvoker(calls), ["foundation.security.seed.state.read", "foundation.security.seed.provider.configure", "foundation.security.seed.handoff.complete"], binding);
    close.push(server.close);
    const base = `${server.origin}/v1/portals/operations/platform/services/core/seed-handoff`;
    expect((await fetch(base, { headers: server.readHeaders })).status).toBe(200);
    expect((await fetch(`${base}/configure-provider`, { method: "POST", headers: server.writeHeaders, body: '{"expectedGeneration":1,"providerProfile":{"id":"enterprise","revision":1,"digest":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}' })).status).toBe(200);
    expect((await fetch(`${base}/verify-provider`, { method: "POST", headers: server.writeHeaders, body: '{"expectedGeneration":2,"providerProfile":{"id":"enterprise","revision":1,"digest":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"assertion":{"forged":true}}' })).status).toBe(409);
    expect(calls.map(({ operation, payload }) => ({ operation, payload }))).toEqual([
      { operation: "get", payload: {} },
      { operation: "configureProvider", payload: { expectedGeneration: 1, providerProfile: { id: "enterprise", revision: 1, digest: "a".repeat(64) } } },
    ]);
  });
});
