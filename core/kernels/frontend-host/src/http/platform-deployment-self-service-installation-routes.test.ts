import { afterEach, describe, expect, it } from "vitest";
import { managementBinding, recordingPlatformInvoker, startPlatformManagementTestServer, type PlatformInvocation } from "../testing/platform-management-test-harness";

const close: Array<() => Promise<void>> = [];
afterEach(async () => Promise.all(close.splice(0).map((action) => action())));

describe("Platform deployment self-service plugin installation routes", () => {
  it("derives every target from ManagementTarget and never accepts a browser target", async () => {
    const calls: PlatformInvocation[] = [];
    const read = ["previewSelfServicePluginInstallation", "listSelfServicePluginInstallationCandidates", "getSelfServicePluginInstallationCandidate"];
    const write = ["createSelfServicePluginInstallationCandidate", "submitSelfServicePluginInstallationCandidate", "approveSelfServicePluginInstallationCandidate"];
    const server = await startPlatformManagementTestServer(
      recordingPlatformInvoker(calls, (_capability, operation) => operation === "listSelfServicePluginInstallationCandidates" ? { items: [] } : {}),
      ["platform.deployment.plugin.preview", "platform.deployment.plugin.request", "platform.deployment.plugin.approve"],
      managementBinding([{ capability: "platform.deployment", read, write }], { kind: "service-unit", kernel: "backend", deployment: "agent-services", unitId: "api" }),
    );
    close.push(server.close);
    const base = `${server.origin}/v1/portals/operations/platform/services/core/deployment/service-plugin-installations`;
    const body = { version: 1, change: { action: "install", pluginId: "cn.example.agent", requirement: { pluginId: "cn.example.agent", constraint: "^2.0.0", channel: "stable" } } };
    expect((await fetch(base, { headers: server.readHeaders })).status).toBe(200);
    expect((await fetch(`${base}/preview`, { method: "POST", headers: server.writeHeaders, body: JSON.stringify(body) })).status).toBe(200);
    expect((await fetch(base, { method: "POST", headers: server.writeHeaders, body: JSON.stringify(body) })).status).toBe(200);
    const id = "installation-0123456789abcdef0123456789abcdef";
    expect((await fetch(`${base}/${id}/submit`, { method: "POST", headers: server.writeHeaders, body: "{}" })).status).toBe(200);
    const evidence = { "review.expected-digest": "a".repeat(64), "review.acknowledged": true, "review.reason": "reviewed" };
    expect((await fetch(`${base}/${id}/approve`, { method: "POST", headers: server.writeHeaders, body: JSON.stringify({ evidence }) })).status).toBe(200);
    const target = { kernel: "backend", deployment: "agent-services", unitId: "api" };
    expect(calls.map(({ operation, payload }) => ({ operation, payload }))).toEqual([
      { operation: "listSelfServicePluginInstallationCandidates", payload: { installationTarget: target } },
      { operation: "previewSelfServicePluginInstallation", payload: { installationPreview: { ...body, target } } },
      { operation: "createSelfServicePluginInstallationCandidate", payload: { installationPreview: { ...body, target } } },
      { operation: "submitSelfServicePluginInstallationCandidate", payload: { candidateId: id, installationTarget: target } },
      { operation: "approveSelfServicePluginInstallationCandidate", payload: { candidateId: id, installationTarget: target, approvalEvidence: evidence } },
    ]);
    expect((await fetch(`${base}/preview`, { method: "POST", headers: server.writeHeaders, body: JSON.stringify({ ...body, target: { kernel: "backend", deployment: "other", unitId: "api" } }) })).status).toBe(400);
    expect(calls).toHaveLength(5);
  });

  it("fails closed when the published management target has no resource scope", async () => {
    const calls: PlatformInvocation[] = [];
    const server = await startPlatformManagementTestServer(recordingPlatformInvoker(calls), ["platform.deployment.plugin.preview"], managementBinding([{ capability: "platform.deployment", read: ["listSelfServicePluginInstallationCandidates"] }]));
    close.push(server.close);
    const response = await fetch(`${server.origin}/v1/portals/operations/platform/services/core/deployment/service-plugin-installations`, { headers: server.readHeaders });
    expect(response.status).toBe(409);
    expect(calls).toEqual([]);
  });
});
