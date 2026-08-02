import { afterEach, describe, expect, it } from "vitest";
import { managementBinding, recordingPlatformInvoker, startPlatformManagementTestServer, type PlatformInvocation } from "../testing/platform-management-test-harness";

const close: Array<() => Promise<void>> = [];
afterEach(async () => Promise.all(close.splice(0).map((action) => action())));

describe("Platform marketplace routes", () => {
  it("uses an opaque configured source id and rejects browser URL injection", async () => {
    const calls: PlatformInvocation[] = [];
    const server = await startPlatformManagementTestServer(
      recordingPlatformInvoker(calls, (_capability, operation) => operation === "listSources" ? { version: 1, sources: [] } : { version: 1, source: {}, revision: 1, total: 0, page: 1, pageSize: 20, items: [] }),
      ["platform.artifacts.marketplace.read"],
      managementBinding([{ capability: "platform.artifacts.marketplace", read: ["listSources", "listCatalog"] }]),
    );
    close.push(server.close);
    const base = `${server.origin}/v1/portals/operations/platform/services/core/marketplace`;
    expect((await fetch(`${base}/sources`, { headers: server.readHeaders })).status).toBe(200);
    expect((await fetch(`${base}/catalog?sourceId=enterprise&target=backend&page=1&pageSize=20`, { headers: server.readHeaders })).status).toBe(200);
    expect((await fetch(`${base}/catalog?sourceId=enterprise&url=https%3A%2F%2Fattacker.example`, { headers: server.readHeaders })).status).toBe(400);
    expect(calls.map(({ operation, payload }) => ({ operation, payload }))).toEqual([
      { operation: "listSources", payload: {} },
      { operation: "listCatalog", payload: { version: 1, sourceId: "enterprise", query: { target: "backend", page: 1, pageSize: 20 } } },
    ]);
  });
});
