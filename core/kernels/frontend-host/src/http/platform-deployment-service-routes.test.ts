import { afterEach, describe, expect, it } from "vitest";
import { managementBinding, recordingPlatformInvoker, startPlatformManagementTestServer, type PlatformInvocation } from "../testing/platform-management-test-harness";

const close: (() => Promise<void>)[] = [];
afterEach(async () => Promise.all(close.splice(0).map((action) => action())));

describe("Platform deployment service revision routes", () => {
  it("maps draft, separation, publication, rollback and audit operations", async () => {
    const calls: PlatformInvocation[] = [];
    const server = await start(calls);
    const base = `${server.origin}/v1/portals/operations/platform/services/core/deployment`;
    for (const path of ["/targets", "/service-revisions"]) expect((await fetch(`${base}${path}`, { headers: server.readHeaders })).status).toBe(200);
    expect((await fetch(`${base}/service-revisions`, { method: "POST", headers: server.writeHeaders, body: '{"composition":{"id":"service-a"}}' })).status).toBe(200);
    expect((await fetch(`${base}/service-revisions/7`, { method: "PUT", headers: server.writeHeaders, body: '{"revisionId":99,"composition":{"id":"service-b"}}' })).status).toBe(200);
    for (const action of ["submit", "approve", "publish", "rollback"]) expect((await fetch(`${base}/service-revisions/7/${action}`, { method: "POST", headers: server.writeHeaders, body: "{}" })).status, action).toBe(200);
    expect((await fetch(`${base}/service-revisions/7/audit`, { headers: server.readHeaders })).status).toBe(200);
    expect(calls.map(({ operation, payload }) => ({ operation, payload }))).toEqual([
      { operation: "listDeploymentTargets", payload: {} }, { operation: "listServiceRevisions", payload: {} },
      { operation: "createServiceDraft", payload: { composition: { id: "service-a" } } },
      { operation: "updateServiceDraft", payload: { revisionId: 7, composition: { id: "service-b" } } },
      { operation: "submitServiceDraft", payload: { revisionId: 7 } }, { operation: "approveServiceRevision", payload: { revisionId: 7 } },
      { operation: "publishServiceRevision", payload: { revisionId: 7 } }, { operation: "rollbackServiceRevision", payload: { revisionId: 7 } },
      { operation: "listServiceRevisionAudit", payload: { revisionId: 7 } },
    ]);
  });
});

async function start(calls: PlatformInvocation[]) {
  const read = ["listDeploymentTargets", "listServiceRevisions", "listServiceRevisionAudit"];
  const write = ["createServiceDraft", "updateServiceDraft", "submitServiceDraft", "approveServiceRevision", "publishServiceRevision", "rollbackServiceRevision"];
  const server = await startPlatformManagementTestServer(recordingPlatformInvoker(calls, (_capability, operation) => read.includes(operation) ? { items: [] } : {}), ["platform.admin"], managementBinding([{ capability: "platform.deployment", read, write }]));
  close.push(server.close);
  return server;
}
