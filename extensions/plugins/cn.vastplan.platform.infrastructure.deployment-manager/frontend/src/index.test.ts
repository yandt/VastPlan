import { describe, expect, it } from "vitest";
import type { ServiceRevision } from "@vastplan/platform-admin";
import { buildBackendIntent, createDeploymentPage, deploymentRow, intentEditorValue, serviceIntentSchema } from "./index";
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
