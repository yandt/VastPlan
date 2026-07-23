import { afterEach, describe, expect, it } from "vitest";
import { managementBinding, recordingPlatformInvoker, startPlatformManagementTestServer, type PlatformInvocation } from "../testing/platform-management-test-harness";

const close: (() => Promise<void>)[] = [];
afterEach(async () => Promise.all(close.splice(0).map((action) => action())));

describe("Platform plugin configuration routes", () => {
  it("uses opaque configuration resources and server-owned management targets", async () => {
    const calls: PlatformInvocation[] = [];
    const server = await startPlatformManagementTestServer(
      recordingPlatformInvoker(calls, (_capability, operation) => operation.startsWith("list") ? { items: [] } : {}),
      ["platform.plugin-configuration.read", "platform.plugin-configuration.write", "platform.plugin-configuration.publish", "platform.plugin-configuration.profile.publish", "platform.plugin-configuration.hot.publish", "platform.plugin-configuration.scoped.publish"],
	  managementBinding([{ capability: "platform.plugin-configuration", read: ["listDefinitions", "getDefinition", "listCandidates"], write: ["createDraft", "discardDraft", "submitDraft", "activateCandidate", "submitProfileDraft", "approveProfileCandidate", "activateProfileCandidate", "abortProfileCandidate", "submitHotServiceDraft", "approveHotServiceCandidate", "activateHotServiceCandidate", "abortHotServiceCandidate", "submitScopedDraft", "approveScopedCandidate", "activateScopedCandidate", "abortScopedCandidate"] }]),
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
    expect((await fetch(`${base}/candidates/pcfg_cccccccccccccccccccccccccccccccc/submit-profile`, { method: "POST", headers: server.writeHeaders, body: '{"expectedRevision":4}' })).status).toBe(200);
    expect((await fetch(`${base}/candidates/pcfg_cccccccccccccccccccccccccccccccc/approve-profile`, { method: "POST", headers: server.writeHeaders, body: '{"expectedRevision":5}' })).status).toBe(200);
    expect((await fetch(`${base}/candidates/pcfg_cccccccccccccccccccccccccccccccc/activate-profile`, { method: "POST", headers: server.writeHeaders, body: '{"expectedRevision":6}' })).status).toBe(200);
    expect((await fetch(`${base}/candidates/pcfg_cccccccccccccccccccccccccccccccc/abort-profile`, { method: "POST", headers: server.writeHeaders, body: '{"expectedRevision":7}' })).status).toBe(200);
    expect((await fetch(`${base}/candidates/pcfg_cccccccccccccccccccccccccccccccc/submit-hot`, { method: "POST", headers: server.writeHeaders, body: '{"expectedRevision":8}' })).status).toBe(200);
    expect((await fetch(`${base}/candidates/pcfg_cccccccccccccccccccccccccccccccc/approve-hot`, { method: "POST", headers: server.writeHeaders, body: '{"expectedRevision":9}' })).status).toBe(200);
    expect((await fetch(`${base}/candidates/pcfg_cccccccccccccccccccccccccccccccc/activate-hot`, { method: "POST", headers: server.writeHeaders, body: '{"expectedRevision":10}' })).status).toBe(200);
    expect((await fetch(`${base}/candidates/pcfg_cccccccccccccccccccccccccccccccc/abort-hot`, { method: "POST", headers: server.writeHeaders, body: '{"expectedRevision":11}' })).status).toBe(200);
	for (const [action, revision] of [["submit-scoped", 12], ["approve-scoped", 13], ["activate-scoped", 14], ["abort-scoped", 15]] as const) {
	  const response = await fetch(`${base}/candidates/pcfg_cccccccccccccccccccccccccccccccc/${action}`, { method: "POST", headers: server.writeHeaders, body: JSON.stringify({ expectedRevision: revision }) });
	  expect({ action, status: response.status }).toEqual({ action, status: 200 });
	}
    expect(calls.map(({ capability, operation, payload }) => ({ capability, operation, payload }))).toEqual([
      { capability: "platform.plugin-configuration", operation: "listDefinitions", payload: {} },
      { capability: "platform.plugin-configuration", operation: "getDefinition", payload: { configurationId: "cfg_aaaaaaaaaaaaaaaaaaaaaaaa", catalogDigest: "b".repeat(64) } },
      { capability: "platform.plugin-configuration", operation: "listCandidates", payload: {} },
	  { capability: "platform.plugin-configuration", operation: "createDraft", payload: { configurationId: "cfg_aaaaaaaaaaaaaaaaaaaaaaaa", catalogDigest: "b".repeat(64), values: { region: "cn-east" }, secrets: { token: "one-use-secret" } } },
      { capability: "platform.plugin-configuration", operation: "discardDraft", payload: { id: "pcfg_cccccccccccccccccccccccccccccccc", expectedRevision: 1 } },
      { capability: "platform.plugin-configuration", operation: "submitDraft", payload: { id: "pcfg_cccccccccccccccccccccccccccccccc", expectedRevision: 2 } },
      { capability: "platform.plugin-configuration", operation: "activateCandidate", payload: { id: "pcfg_cccccccccccccccccccccccccccccccc", expectedRevision: 3 } },
      { capability: "platform.plugin-configuration", operation: "submitProfileDraft", payload: { id: "pcfg_cccccccccccccccccccccccccccccccc", expectedRevision: 4 } },
      { capability: "platform.plugin-configuration", operation: "approveProfileCandidate", payload: { id: "pcfg_cccccccccccccccccccccccccccccccc", expectedRevision: 5 } },
      { capability: "platform.plugin-configuration", operation: "activateProfileCandidate", payload: { id: "pcfg_cccccccccccccccccccccccccccccccc", expectedRevision: 6 } },
      { capability: "platform.plugin-configuration", operation: "abortProfileCandidate", payload: { id: "pcfg_cccccccccccccccccccccccccccccccc", expectedRevision: 7 } },
      { capability: "platform.plugin-configuration", operation: "submitHotServiceDraft", payload: { id: "pcfg_cccccccccccccccccccccccccccccccc", expectedRevision: 8 } },
      { capability: "platform.plugin-configuration", operation: "approveHotServiceCandidate", payload: { id: "pcfg_cccccccccccccccccccccccccccccccc", expectedRevision: 9 } },
      { capability: "platform.plugin-configuration", operation: "activateHotServiceCandidate", payload: { id: "pcfg_cccccccccccccccccccccccccccccccc", expectedRevision: 10 } },
      { capability: "platform.plugin-configuration", operation: "abortHotServiceCandidate", payload: { id: "pcfg_cccccccccccccccccccccccccccccccc", expectedRevision: 11 } },
	  { capability: "platform.plugin-configuration", operation: "submitScopedDraft", payload: { id: "pcfg_cccccccccccccccccccccccccccccccc", expectedRevision: 12 } },
	  { capability: "platform.plugin-configuration", operation: "approveScopedCandidate", payload: { id: "pcfg_cccccccccccccccccccccccccccccccc", expectedRevision: 13 } },
	  { capability: "platform.plugin-configuration", operation: "activateScopedCandidate", payload: { id: "pcfg_cccccccccccccccccccccccccccccccc", expectedRevision: 14 } },
	  { capability: "platform.plugin-configuration", operation: "abortScopedCandidate", payload: { id: "pcfg_cccccccccccccccccccccccccccccccc", expectedRevision: 15 } },
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

  it("routes resource-profile reads and mutations through governed operations", async () => {
    const calls: PlatformInvocation[] = [];
    const operations = ["listResourceItems", "getResourceItem", "createResourceDraft", "updateResourceDraft", "deleteResourceDraft", "submitResourceDraft", "approveResourceCandidate", "activateResourceCandidate", "abortResourceCandidate"];
    const server = await startPlatformManagementTestServer(
      recordingPlatformInvoker(calls, (_capability, operation) => operation.startsWith("list") ? { items: [] } : {}),
      ["platform.plugin-configuration.read", "platform.plugin-configuration.write", "platform.plugin-configuration.resource.publish"],
      managementBinding([{ capability: "platform.plugin-configuration", read: operations.slice(0, 2), write: operations.slice(2) }]),
    );
    close.push(server.close);
    const base = `${server.origin}/v1/portals/operations/platform/services/core/plugin-configurations`;
    const configurationId = "cfg_aaaaaaaaaaaaaaaaaaaaaaaa", resourceCollectionId = "cfgc_bbbbbbbbbbbbbbbbbbbbbbbb", resourceId = "cfgp_cccccccccccccccccccccccc", catalogDigest = "d".repeat(64);
    const query = `configurationId=${configurationId}&resourceCollectionId=${resourceCollectionId}&catalogDigest=${catalogDigest}`;
    expect((await fetch(`${base}/resources?${query}`, { headers: server.readHeaders })).status).toBe(200);
    expect((await fetch(`${base}/resources/${resourceId}?${query}`, { headers: server.readHeaders })).status).toBe(200);
    for (const action of ["create", "update", "delete"]) {
      expect((await fetch(`${base}/resources/candidates/${action}`, { method: "POST", headers: server.writeHeaders, body: JSON.stringify({ configurationId, resourceCollectionId, resourceId, catalogDigest, values: { endpoint: "https://notify.example.test" } }) })).status).toBe(200);
    }
    const candidateId = "pcfg_eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee";
    for (const action of ["submit-resource", "approve-resource", "activate-resource", "abort-resource"]) {
      expect((await fetch(`${base}/candidates/${candidateId}/${action}`, { method: "POST", headers: server.writeHeaders, body: '{"expectedRevision":4}' })).status).toBe(200);
    }
    expect(calls.map((call) => call.operation)).toEqual(operations);
    expect(calls[0]?.payload).toEqual({ configurationId, resourceCollectionId, catalogDigest });
    expect(calls[1]?.payload).toEqual({ configurationId, resourceCollectionId, catalogDigest, resourceId });
    expect(calls.every((call) => call.logicalService === "platform.core.primary")).toBe(true);
  });
});
