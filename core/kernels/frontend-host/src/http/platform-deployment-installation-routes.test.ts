import { afterEach, describe, expect, it } from "vitest";
import { managementBinding, recordingPlatformInvoker, startPlatformManagementTestServer, type PlatformInvocation } from "../testing/platform-management-test-harness";

const close: Array<() => Promise<void>> = [];
afterEach(async () => Promise.all(close.splice(0).map((action) => action())));

describe("Platform deployment plugin installation routes", () => {
  it("maps controller preview and candidate lifecycle without accepting a browser source", async () => {
    const calls: PlatformInvocation[] = [];
    const server = await start(calls, ["platform.deployment.plugin.preview", "platform.deployment.plugin.request", "platform.deployment.plugin.approve", "platform.deployment.plugin.activate"]);
    const base = `${server.origin}/v1/portals/operations/platform/services/core/deployment/plugin-installations`;
    const body = JSON.stringify(previewRequest());
    expect((await fetch(base, { headers: server.readHeaders })).status).toBe(200);
    expect((await fetch(`${base}/targets`, { headers: server.readHeaders })).status).toBe(200);
    expect((await fetch(`${base}/preview`, { method: "POST", headers: server.writeHeaders, body })).status).toBe(200);
    expect((await fetch(base, { method: "POST", headers: server.writeHeaders, body })).status).toBe(200);
    const id = "installation-0123456789abcdef0123456789abcdef";
    expect((await fetch(`${base}/${id}`, { headers: server.readHeaders })).status).toBe(200);
    for (const action of ["submit", "approve", "activate", "cancel", "rollback"]) {
      expect((await fetch(`${base}/${id}/${action}`, { method: "POST", headers: server.writeHeaders, body: "{}" })).status).toBe(200);
    }
    expect(calls.map(({ operation, payload }) => ({ operation, payload }))).toEqual([
      { operation: "listPluginInstallationCandidates", payload: {} },
      { operation: "listPluginInstallationTargets", payload: {} },
      { operation: "previewPluginInstallation", payload: { installationPreview: previewRequest() } },
      { operation: "createPluginInstallationCandidate", payload: { installationPreview: previewRequest() } },
      { operation: "getPluginInstallationCandidate", payload: { candidateId: id } },
      ...[
        "submitPluginInstallationCandidate", "approvePluginInstallationCandidate", "activatePluginInstallationCandidate",
        "cancelPluginInstallationCandidate", "rollbackPluginInstallationCandidate",
      ].map((operation) => ({ operation, payload: { candidateId: id } })),
    ]);
  });

  it("rejects browser-selected sources and malformed target fields before invoking a capability", async () => {
    const calls: PlatformInvocation[] = [];
    const server = await start(calls, ["platform.deployment.plugin.preview"]);
    const base = `${server.origin}/v1/portals/operations/platform/services/core/deployment/plugin-installations/preview`;
    expect((await fetch(base, { method: "POST", headers: server.writeHeaders, body: JSON.stringify({ ...previewRequest(), source: "development" }) })).status).toBe(400);
    expect((await fetch(base, { method: "POST", headers: server.writeHeaders, body: JSON.stringify({ ...previewRequest(), target: { kernel: "backend", deployment: "../other", unitId: "api" } }) })).status).toBe(400);
    expect(calls).toEqual([]);
  });

  it("separates preview, request, approval and activation permissions", async () => {
    const calls: PlatformInvocation[] = [];
    const server = await start(calls, ["platform.deployment.plugin.preview"]);
    const base = `${server.origin}/v1/portals/operations/platform/services/core/deployment/plugin-installations`;
    expect((await fetch(`${base}/preview`, { method: "POST", headers: server.writeHeaders, body: JSON.stringify(previewRequest()) })).status).toBe(200);
    expect((await fetch(base, { method: "POST", headers: server.writeHeaders, body: JSON.stringify(previewRequest()) })).status).toBe(403);
    expect(calls.map((call) => call.operation)).toEqual(["previewPluginInstallation"]);
  });
});

function previewRequest() {
  return {
    version: 1,
    target: { kernel: "backend", deployment: "agent-services", unitId: "api" },
    change: { action: "upgrade", pluginId: "cn.example.agent", requirement: { pluginId: "cn.example.agent", constraint: "^2.0.0", channel: "stable" } },
    expectedActiveRevision: 7,
  };
}

async function start(calls: PlatformInvocation[], roles: string[]) {
  const read = ["previewPluginInstallation", "listPluginInstallationTargets", "listPluginInstallationCandidates", "getPluginInstallationCandidate"];
  const write = ["createPluginInstallationCandidate", "submitPluginInstallationCandidate", "approvePluginInstallationCandidate", "activatePluginInstallationCandidate", "cancelPluginInstallationCandidate", "rollbackPluginInstallationCandidate"];
  const server = await startPlatformManagementTestServer(
    recordingPlatformInvoker(calls, (_capability, operation) => operation === "listPluginInstallationCandidates" || operation === "listPluginInstallationTargets" ? { items: [] } : {}),
    roles,
    managementBinding([{ capability: "platform.deployment", read, write }]),
  );
  close.push(server.close);
  return server;
}
