import { afterEach, describe, expect, it } from "vitest";
import { managementBinding, recordingPlatformInvoker, startPlatformManagementTestServer, type PlatformInvocation } from "../testing/platform-management-test-harness";

const close: (() => Promise<void>)[] = [];
afterEach(async () => Promise.all(close.splice(0).map((action) => action())));

describe("Platform plugin configuration routes", () => {
  it("uses opaque configuration resources and server-owned management targets", async () => {
    const calls: PlatformInvocation[] = [];
    const server = await startPlatformManagementTestServer(
      recordingPlatformInvoker(calls, (_capability, operation) => operation.startsWith("list") ? { items: [] } : {}),
      ["platform.plugin-configuration.read", "platform.plugin-configuration.write", "platform.plugin-configuration.publish"],
      managementBinding([{ capability: "platform.plugin-configuration", read: ["listDefinitions", "getDefinition", "listCandidates"], write: ["createDraft", "discardDraft", "submitDraft", "activateCandidate"] }]),
    );
    close.push(server.close);
    const base = `${server.origin}/v1/portals/operations/platform/services/core/plugin-configurations`;
    expect((await fetch(base, { headers: server.readHeaders })).status).toBe(200);
    expect((await fetch(`${base}/cfg_aaaaaaaaaaaaaaaaaaaaaaaa?catalogDigest=${"b".repeat(64)}`, { headers: server.readHeaders })).status).toBe(200);
    expect((await fetch(`${base}/candidates`, { headers: server.readHeaders })).status).toBe(200);
	expect((await fetch(`${base}/candidates`, { method: "POST", headers: server.writeHeaders, body: JSON.stringify({ configurationId: "cfg_aaaaaaaaaaaaaaaaaaaaaaaa", catalogDigest: "b".repeat(64), values: { region: "cn-east" }, secrets: { token: "one-use-secret" } }) })).status).toBe(200);
    expect((await fetch(`${base}/candidates/pcfg_cccccccccccccccccccccccccccccccc`, { method: "DELETE", headers: server.writeHeaders, body: '{"id":"forged","expectedRevision":1}' })).status).toBe(200);
    expect((await fetch(`${base}/candidates/pcfg_cccccccccccccccccccccccccccccccc/submit`, { method: "POST", headers: server.writeHeaders, body: '{"expectedRevision":2}' })).status).toBe(200);
    expect((await fetch(`${base}/candidates/pcfg_cccccccccccccccccccccccccccccccc/activate`, { method: "POST", headers: server.writeHeaders, body: '{"expectedRevision":3}' })).status).toBe(200);
    expect(calls.map(({ capability, operation, payload }) => ({ capability, operation, payload }))).toEqual([
      { capability: "platform.plugin-configuration", operation: "listDefinitions", payload: {} },
      { capability: "platform.plugin-configuration", operation: "getDefinition", payload: { configurationId: "cfg_aaaaaaaaaaaaaaaaaaaaaaaa", catalogDigest: "b".repeat(64) } },
      { capability: "platform.plugin-configuration", operation: "listCandidates", payload: {} },
	  { capability: "platform.plugin-configuration", operation: "createDraft", payload: { configurationId: "cfg_aaaaaaaaaaaaaaaaaaaaaaaa", catalogDigest: "b".repeat(64), values: { region: "cn-east" }, secrets: { token: "one-use-secret" } } },
      { capability: "platform.plugin-configuration", operation: "discardDraft", payload: { id: "pcfg_cccccccccccccccccccccccccccccccc", expectedRevision: 1 } },
      { capability: "platform.plugin-configuration", operation: "submitDraft", payload: { id: "pcfg_cccccccccccccccccccccccccccccccc", expectedRevision: 2 } },
      { capability: "platform.plugin-configuration", operation: "activateCandidate", payload: { id: "pcfg_cccccccccccccccccccccccccccccccc", expectedRevision: 3 } },
    ]);
    expect(calls.every((call) => call.logicalService === "platform.core.primary")).toBe(true);
  });

  it("requires both Management Binding and role", async () => {
    const calls: PlatformInvocation[] = [];
    const server = await startPlatformManagementTestServer(recordingPlatformInvoker(calls), ["platform.plugin-configuration.read"], managementBinding([{ capability: "platform.plugin-configuration", read: ["listDefinitions"] }]));
    close.push(server.close);
    const base = `${server.origin}/v1/portals/operations/platform/services/core/plugin-configurations`;
    expect((await fetch(`${base}/candidates`, { headers: server.readHeaders })).status).toBe(403);
    expect((await fetch(base, { method: "POST", headers: server.writeHeaders, body: "{}" })).status).toBe(405);
    expect(calls).toHaveLength(0);
  });
});
