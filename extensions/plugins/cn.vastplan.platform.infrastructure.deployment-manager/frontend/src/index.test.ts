import { describe, expect, it } from "vitest";
import type { PluginInstallationCandidate, ServiceRevision } from "@vastplan/platform-admin";
import { buildBackendIntent, buildInstallationRequests, createDeploymentPage, createPluginInstallationPage, deploymentRow, installationRow, installationTargetChoices, intentEditorValue, serviceIntentSchema } from "./index";
import type { PlatformAdminClient } from "@vastplan/platform-admin";
import type { DeploymentRow } from "./resolution-view";

describe("deployment-manager Intent frontend contract", () => {
  it("exposes only user-owned Intent fields", () => {
    const schema = serviceIntentSchema([{ deploymentName: "agent-services", platformProfile: { id: "baseline", revision: 1, digest: "a".repeat(64) } }]);
    const encoded = JSON.stringify(schema.schema);
    expect(schema.id).toBe("backend-application-intent.v1");
    expect(encoded).toContain("rootPlugins");
    expect(encoded).toContain("features");
    expect(encoded).toContain("versionPolicy");
    expect(encoded).not.toContain("dependsOn");
    expect(encoded).not.toContain("instancePolicy");
    expect(encoded).not.toContain("stateModel");
    expect(encoded).not.toContain("routingDomain");
    expect(encoded).not.toContain("logicalService");
  });

  it("builds a closed Application Intent instead of an Application Composition", () => {
    const intent = buildBackendIntent({
      deployment: "agent-services",
      services: [{
        serviceClass: "application.backend", id: "api", replicas: 3,
        rootPlugins: [{ pluginId: "cn.example.agent", versionPolicy: "compatible", version: "1.0.0", channel: "stable", features: ["audit"] }],
        pluginConfig: { "cn.example.agent": { mode: "safe" } },
        autoscaling: { minReplicas: 2, maxReplicas: 5, metric: "requests", targetValuePerReplica: 50 },
        resources: { cpuMillis: 500, memoryBytes: 268435456 }, nodeSelector: { zone: "primary" },
      }],
    });
    expect(intent.metadata).toEqual({ name: "agent-services" });
    expect(intent.services[0]).toEqual({
      id: "api", serviceClass: "application.backend",
      rootPlugins: [{ pluginId: "cn.example.agent", constraint: "^1.0.0", channel: "stable", features: ["audit"] }],
      pluginConfig: { "cn.example.agent": { mode: "safe" } },
      operations: {
        replicas: 3,
        autoscaling: { min_replicas: 2, max_replicas: 5, metric: "requests", target_value_per_replica: 50 },
        resources: { requests: { cpu_millis: 500, memory_bytes: 268435456 } },
        placement: { nodeSelector: { zone: "primary" } },
      },
    });
    expect(intent.services[0]).not.toHaveProperty("depends_on");
  });

  it("round-trips editable Intent state and marks legacy revisions read-only", () => {
    const intent = buildBackendIntent({ deployment: "agent-services", services: [{ serviceClass: "application.backend", id: "api", replicas: 1, rootPlugins: [{ pluginId: "cn.example.agent", versionPolicy: "exact", version: "1.0.0", channel: "stable" }] }] });
    const revision = { id: 7, deployment: "agent-services", status: "Draft", active: false, intent, resolutionReport: { status: "Resolved" }, composition: { units: [] }, preview: {}, previewDigest: "p", artifactReferences: [], createdAt: "now", updatedAt: "now" } as unknown as ServiceRevision;
    expect(intentEditorValue(revision)).toMatchObject({ deployment: "agent-services", services: [{ id: "api", replicas: 1, rootPlugins: [{ versionPolicy: "exact", version: "1.0.0" }] }] });
    expect(deploymentRow(revision)).toMatchObject({ revisionKind: "Intent", planStatus: "Resolved", planningStale: false });
    expect(deploymentRow({ ...revision, intent: undefined, resolutionReport: undefined })).toMatchObject({ revisionKind: "Legacy", planStatus: "Legacy" });
  });

  it("keeps legacy revisions out of every mutating page action", () => {
    const page = createDeploymentPage({} as PlatformAdminClient, "deployment", "/deployment", "Deployment");
    const actions = Object.fromEntries((page.collection.actions ?? []).map((action) => [action.id, action]));
    expect(page.pageActions?.find((action) => action.id === "create")?.form).toBe("create");
    for (const id of ["edit", "refresh-plan", "submit", "approve", "publish", "rollback"]) {
      expect(JSON.stringify(actions[id]?.visibleWhen), id).toContain('/revisionKind');
      expect(JSON.stringify(actions[id]?.visibleWhen), id).toContain('"Intent"');
    }
    expect(page.forms?.every((form) => form.schema.id === "backend-application-intent.v1")).toBe(true);
  });

  it("dispatches mutation actions through the action dictionary", async () => {
    const calls: string[] = [];
    const client = {
      async refreshIntentDraft(id: number) { calls.push(`refresh-plan:${id}`); },
      async submitServiceDraft(id: number) { calls.push(`submit:${id}`); },
      async approveServiceRevision(id: number) { calls.push(`approve:${id}`); },
      async publishServiceRevision(id: number) { calls.push(`publish:${id}`); },
      async rollbackServiceRevision(id: number) { calls.push(`rollback:${id}`); },
    } as unknown as PlatformAdminClient;
    const page = createDeploymentPage(client, "deployment", "/deployment", "Deployment");
    const selected = [{ id: 7 } as DeploymentRow];
    for (const id of ["refresh-plan", "submit", "approve", "publish", "rollback"]) {
      await page.runAction?.({ action: { id, label: id, icon: "more", placement: "record.row" }, selected, refresh() {} }, new AbortController().signal);
    }
    expect(calls).toEqual(["refresh-plan:7", "submit:7", "approve:7", "publish:7", "rollback:7"]);
  });
});

describe("deployment-manager controller installation frontend", () => {
  it("derives selectable logical service targets only from active Application Intents", () => {
    expect(installationTargetChoices([{ target: { kernel: "backend", deployment: "agents", unitId: "api" }, serviceClass: "application.backend", activeRevision: 7 }])).toEqual([
      { key: "agents#api", title: "agents · api", deployment: "agents", unitId: "api", activeRevision: 7 },
    ]);
  });

  it("builds one bounded controller request per deployment and never accepts a source", () => {
    const targets = {
      "agents#api": { key: "agents#api", title: "agents · api", deployment: "agents", unitId: "api", activeRevision: 7 },
      "reports#worker": { key: "reports#worker", title: "reports · worker", deployment: "reports", unitId: "worker", activeRevision: 3 },
    };
    const requests = buildInstallationRequests({ targets: Object.keys(targets), action: "upgrade", pluginId: "cn.example.agent", versionPolicy: "compatible", version: "2.1.0", channel: "stable", features: ["audit"] }, targets);
    expect(requests).toHaveLength(2);
    expect(requests[0]).toMatchObject({ version: 1, target: { kernel: "backend", deployment: "agents", unitId: "api" }, change: { action: "upgrade", requirement: { constraint: "^2.1.0" } }, expectedActiveRevision: 7 });
    expect(requests[0]).not.toHaveProperty("source");
    expect(() => buildInstallationRequests({ targets: ["agents#api", "agents#worker"], action: "remove", pluginId: "cn.example.agent" }, {
      ...targets, "agents#worker": { key: "agents#worker", title: "agents · worker", deployment: "agents", unitId: "worker", activeRevision: 7 },
    })).toThrow("同一部署");
  });

  it("projects candidate lifecycle and rollout independently", () => {
    const candidate = {
      id: "installation-a", status: "Ready", source: "controller", serviceRevisionId: 8, previousServiceRevisionId: 7,
      requestedBy: "alice", createdAt: "now", updatedAt: "now",
      preview: { target: { kernel: "backend", deployment: "agents", unitId: "api" }, pluginId: "cn.example.agent", action: "upgrade", artifactLock: { roots: [{ pluginId: "cn.example.agent", constraint: "^2.0.0" }] } },
      rollout: { status: "Pending", units: [{ id: "api", desired_replicas: 3, replicas: 2, ready_replicas: 1 }] },
    } as unknown as PluginInstallationCandidate;
    expect(installationRow(candidate)).toMatchObject({ status: "Ready", rolloutStatus: "Pending", rolloutReplicas: "1/3", version: "^2.0.0", hasRollout: true });
  });

  it("keeps every mutation data-driven and protected by its dedicated permission", () => {
    const page = createPluginInstallationPage({} as PlatformAdminClient, undefined, "operations", "deployment", "/plugins", "服务插件");
    expect(page.pageActions?.[0]).toMatchObject({ form: "create-plugin-installation", requiredPermissions: ["platform.deployment.plugin.request"] });
    const actions = Object.fromEntries((page.collection.actions ?? []).map((action) => [action.id, action]));
    expect(actions.submit?.requiredPermissions).toEqual(["platform.deployment.plugin.request"]);
    expect(actions.approve?.requiredPermissions).toEqual(["platform.deployment.plugin.approve"]);
    expect(actions.activate?.requiredPermissions).toEqual(["platform.deployment.plugin.activate"]);
    expect(actions.preview?.overlay).toBe("preview");
  });
});
