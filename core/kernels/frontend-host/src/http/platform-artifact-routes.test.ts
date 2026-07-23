import { afterEach, describe, expect, it } from "vitest";
import { managementBinding, recordingPlatformInvoker, startPlatformManagementTestServer, type PlatformInvocation } from "../testing/platform-management-test-harness";

const close: (() => Promise<void>)[] = [];
afterEach(async () => Promise.all(close.splice(0).map((action) => action())));

describe("Platform artifact routes", () => {
  it("maps bounded repository queries to fixed read operations", async () => {
    const calls: PlatformInvocation[] = [];
    const server = await start(calls);
    for (const path of [
      "/artifacts/status",
      "/artifacts/catalog?pluginPrefix=cn.vastplan&target=frontend&lifecycle=active&page=2&pageSize=10",
      "/artifacts/capacity", "/artifacts/references", "/artifacts/migration", "/artifacts/gc/plan", "/artifacts/gc/status", "/artifacts/publications",
      "/artifacts/evidence?pluginId=cn.vastplan.demo&version=1.0.0&channel=testing",
    ]) expect((await fetch(`${server.origin}/v1/portals/operations/platform/services/core${path}`, { headers: server.readHeaders })).status, path).toBe(200);
    expect(calls.map(({ operation, payload }) => ({ operation, payload }))).toEqual([
      { operation: "status", payload: {} },
      { operation: "listCatalog", payload: { pluginPrefix: "cn.vastplan", target: "frontend", lifecycle: "active", page: 2, pageSize: 10 } },
      { operation: "capacity", payload: {} }, { operation: "listReferences", payload: {} }, { operation: "migrationStatus", payload: {} },
      { operation: "gcPlan", payload: {} }, { operation: "gcStatus", payload: {} },
      { operation: "listPublications", payload: {} }, { operation: "getSupplyChainEvidence", payload: { pluginId: "cn.vastplan.demo", version: "1.0.0", channel: "testing" } },
    ]);
    expect(calls.every((call) => call.capability === "platform.artifacts.repository")).toBe(true);
  });

  it("isolates lifecycle, GC and migration mutation workflows", async () => {
    const calls: PlatformInvocation[] = [];
    const server = await start(calls);
    const requests: [string, string][] = [
      ["/artifacts/lifecycle", '{"status":"yanked","reason":"unsafe"}'],
      ["/artifacts/gc/quarantine", `{"planId":"${"a".repeat(64)}","graceHours":72}`],
      ["/artifacts/gc/sweep", "{}"],
      ["/artifacts/migrations", '{"migrationId":"move-1","targetProvider":"file","targetVolumeId":"next"}'],
      ["/artifacts/migrations/move-1/sync", "{}"],
      ["/artifacts/migrations/move-1/cutover", '{"migrationId":"forged","observationSeconds":300}'],
      ["/artifacts/migrations/move-1/rollback", "{}"],
      ["/artifacts/migrations/move-1/finalize", "{}"],
      ["/artifacts/migrations/move-1/release", "{}"],
      ["/artifacts/publications", '{"source":{"pluginId":"cn.vastplan.demo","version":"1.0.0","channel":"testing"},"targetChannel":"stable","reason":"ready","expectedRevision":0}'],
      [`/artifacts/publications/${"b".repeat(64)}/approve`, '{"expectedRevision":1,"id":"forged"}'],
      [`/artifacts/publications/${"c".repeat(64)}/reject`, '{"expectedRevision":2,"reason":"unsafe","id":"forged"}'],
      [`/artifacts/publications/${"d".repeat(64)}/cancel`, '{"expectedRevision":3,"reason":"superseded","id":"forged"}'],
    ];
    for (const [path, body] of requests) expect((await fetch(`${server.origin}/v1/portals/operations/platform/services/core${path}`, { method: "POST", headers: server.writeHeaders, body })).status, path).toBe(200);
    expect(calls.map(({ operation, payload }) => ({ operation, payload }))).toEqual([
      { operation: "setLifecycle", payload: { status: "yanked", reason: "unsafe" } },
      { operation: "gcQuarantine", payload: { planId: "a".repeat(64), graceHours: 72 } },
      { operation: "gcSweep", payload: {} },
      { operation: "prepareMigration", payload: { migrationId: "move-1", targetProvider: "file", targetVolumeId: "next" } },
      { operation: "syncMigration", payload: { migrationId: "move-1" } },
      { operation: "cutoverMigration", payload: { migrationId: "move-1", observationSeconds: 300 } },
      { operation: "rollbackMigration", payload: { migrationId: "move-1" } },
      { operation: "finalizeMigration", payload: { migrationId: "move-1" } },
      { operation: "releaseMigration", payload: { migrationId: "move-1" } },
      { operation: "submitPublication", payload: { source: { pluginId: "cn.vastplan.demo", version: "1.0.0", channel: "testing" }, targetChannel: "stable", reason: "ready", expectedRevision: 0 } },
      { operation: "approvePublication", payload: { expectedRevision: 1, id: "b".repeat(64) } },
      { operation: "rejectPublication", payload: { expectedRevision: 2, reason: "unsafe", id: "c".repeat(64) } },
      { operation: "cancelPublication", payload: { expectedRevision: 3, reason: "superseded", id: "d".repeat(64) } },
    ]);
    const before = calls.length;
    const invalidEmpty = await fetch(`${server.origin}/v1/portals/operations/platform/services/core/artifacts/gc/sweep`, { method: "POST", headers: server.writeHeaders, body: '{"force":true}' });
    expect(invalidEmpty.status).toBe(400);
    expect(calls).toHaveLength(before);
  });

  it("rejects duplicate catalog query keys and ungranted operations before invocation", async () => {
    const calls: PlatformInvocation[] = [];
    const server = await startPlatformManagementTestServer(recordingPlatformInvoker(calls), ["platform.artifacts.read"], managementBinding([{ capability: "platform.artifacts.repository", read: ["status", "listCatalog"] }]));
    close.push(server.close);
    const duplicate = await fetch(`${server.origin}/v1/portals/operations/platform/services/core/artifacts/catalog?page=1&page=2`, { headers: server.readHeaders });
    expect(duplicate.status).toBe(400);
    const denied = await fetch(`${server.origin}/v1/portals/operations/platform/services/core/artifacts/capacity`, { headers: server.readHeaders });
    expect(denied.status).toBe(403);
    expect(calls).toEqual([]);
  });
});

async function start(calls: PlatformInvocation[]) {
  const server = await startPlatformManagementTestServer(recordingPlatformInvoker(calls), ["platform.artifacts.read", "platform.artifacts.lifecycle", "platform.artifacts.gc", "platform.artifacts.migrate", "platform.artifacts.publication.submit", "platform.artifacts.publication.approve"], managementBinding([{
    capability: "platform.artifacts.repository",
    read: ["status", "listCatalog", "capacity", "listReferences", "migrationStatus", "gcPlan", "gcStatus", "listPublications", "getSupplyChainEvidence"],
    write: ["setLifecycle", "gcQuarantine", "gcSweep", "prepareMigration", "syncMigration", "cutoverMigration", "rollbackMigration", "finalizeMigration", "releaseMigration", "submitPublication", "approvePublication", "rejectPublication", "cancelPublication"],
  }]));
  close.push(server.close);
  return server;
}
