import { describe, expect, it, vi } from "vitest";
import type { PlatformAdminClient, PluginConfigurationDefinition } from "@vastplan/platform-admin";
import { message } from "@vastplan/workbench-sdk";
import { createPluginConfigurationPage } from "./index";

const definition: PluginConfigurationDefinition = {
  id: "cfg_aaaaaaaaaaaaaaaaaaaaaaaa", deployment: "agent-services", unitId: "api", pluginId: "com.example.configured", pluginName: "Configured",
  origin: "application", artifact: { version: "1.0.0", channel: "stable", sha256: "a".repeat(64) }, scope: "service", applyMode: "restart", applyPath: "application-deployment",
  controllerAvailable: false,
  schema: { type: "object", additionalProperties: false, required: ["region"], properties: { region: { type: "string" } } }, schemaDigest: "b".repeat(64), managedCredentials: [],
  values: { region: "cn-east" }, deploymentRevision: 7, deploymentDigest: "c".repeat(64), catalogDigest: "d".repeat(64),
};

describe("plugin configuration workbench", () => {
  it("renders trusted definitions and prepares the selected signed schema", async () => {
    const createDraft = vi.fn(async () => ({ id: "pcfg_x" }));
    const client = {
      listPluginConfigurationDefinitions: vi.fn(async () => [definition]),
      listPluginConfigurationCandidates: vi.fn(async () => []),
      createPluginConfigurationDraft: createDraft,
      discardPluginConfigurationDraft: vi.fn(),
    } as unknown as PlatformAdminClient;
    const page = createPluginConfigurationPage(client, "configuration", "/settings/plugin-configurations", message("test", "title", "Plugin configuration"));
    const result = await page.load({ mode: "page", page: 1, pageSize: 20, filters: {} }, new AbortController().signal);
    expect(result.items).toHaveLength(1);
    expect(result.items[0]).toMatchObject({ pluginName: "Configured", candidateStatus: "None" });
    const form = page.forms?.[0];
    const prepared = await form?.prepare?.(result.items, new AbortController().signal);
	expect(prepared?.schema?.schema).toMatchObject({ properties: { values: definition.schema } });
	await form?.submit({ value: { values: { region: "cn-west" }, secrets: {} }, selected: result.items }, new AbortController().signal);
	expect(createDraft).toHaveBeenCalledWith(definition.id, definition.catalogDigest, { region: "cn-west" }, {});
  });

  it("renders declared managed fields as one-use secret material and submits them separately", async () => {
	const createDraft = vi.fn(async () => ({ id: "pcfg_x" }));
	const managed = { ...definition, managedCredentials: [{ id: "token", title: "API token", description: "Write only", purpose: "remote.token", required: true }] };
	const client = { listPluginConfigurationDefinitions: vi.fn(async () => [managed]), listPluginConfigurationCandidates: vi.fn(async () => []), createPluginConfigurationDraft: createDraft } as unknown as PlatformAdminClient;
	const page = createPluginConfigurationPage(client, "configuration", "/settings/plugin-configurations", message("test", "title", "Plugin configuration"));
	const result = await page.load({ mode: "page", page: 1, pageSize: 20, filters: {} }, new AbortController().signal);
	const form = page.forms?.[0];
	const prepared = await form?.prepare?.(result.items, new AbortController().signal);
	expect(prepared?.schema?.schema).toMatchObject({ properties: { secrets: { required: ["token"], properties: { token: { format: "vastplan-secret-material", writeOnly: true } } } } });
	await form?.submit({ value: { values: { region: "cn-west" }, secrets: { token: "one-use-secret" } }, selected: result.items }, new AbortController().signal);
	expect(createDraft).toHaveBeenCalledWith(definition.id, definition.catalogDigest, { region: "cn-west" }, { token: "one-use-secret" });
  });

  it("allows a configured required credential to be retained without re-entry", async () => {
    const managed = { ...definition, managedCredentials: [{ id: "token", title: "API token", purpose: "remote.token", required: true }], credentialStates: [{ fieldId: "token", configured: true, version: 2 }] };
    const client = { listPluginConfigurationDefinitions: vi.fn(async () => [managed]), listPluginConfigurationCandidates: vi.fn(async () => []) } as unknown as PlatformAdminClient;
    const page = createPluginConfigurationPage(client, "configuration", "/settings/plugin-configurations", message("test", "title", "Plugin configuration"));
    const result = await page.load({ mode: "page", page: 1, pageSize: 20, filters: {} }, new AbortController().signal);
    const prepared = await page.forms?.[0]?.prepare?.(result.items, new AbortController().signal);
    expect((prepared?.schema?.schema.properties as Record<string, Record<string, unknown>>).secrets.required).toBeUndefined();
  });

  it("treats a legacy omitted managed credential list as empty", async () => {
    const client = {
      listPluginConfigurationDefinitions: vi.fn(async () => [{ ...definition, managedCredentials: undefined }]),
      listPluginConfigurationCandidates: vi.fn(async () => []),
    } as unknown as PlatformAdminClient;
    const page = createPluginConfigurationPage(client, "configuration", "/settings/plugin-configurations", message("test", "title", "Plugin configuration"));
    const result = await page.load({ mode: "page", page: 1, pageSize: 20, filters: {} }, new AbortController().signal);
    expect(result.items[0]?.managedCredentialCount).toBe(0);
  });

  it("discards only the selected draft with its CAS revision", async () => {
    const discard = vi.fn(async () => ({ id: "pcfg_x" }));
    const client = {
      listPluginConfigurationDefinitions: vi.fn(async () => [definition]),
      listPluginConfigurationCandidates: vi.fn(async () => [{
        id: "pcfg_x", configurationId: definition.id, revision: 3, status: "Draft", catalogDigest: definition.catalogDigest,
        schemaDigest: definition.schemaDigest, artifactSha256: definition.artifact.sha256, values: definition.values,
        createdBy: "alice", createdAt: "2026-07-23T00:00:00Z", updatedAt: "2026-07-23T00:00:00Z",
      }]),
      createPluginConfigurationDraft: vi.fn(), discardPluginConfigurationDraft: discard,
    } as unknown as PlatformAdminClient;
    const page = createPluginConfigurationPage(client, "configuration", "/settings/plugin-configurations", message("test", "title", "Plugin configuration"));
    const result = await page.load({ mode: "page", page: 1, pageSize: 20, filters: {} }, new AbortController().signal);
    await page.runAction?.({ action: { id: "discard", label: "Discard", placement: "record.row" }, selected: result.items, refresh() {} }, new AbortController().signal);
    expect(discard).toHaveBeenCalledWith("pcfg_x", 3);
  });

  it("routes Platform Profile actions through the dedicated permission operations", async () => {
    const submit = vi.fn(), approve = vi.fn(), activate = vi.fn(), abort = vi.fn();
    const platformDefinition = { ...definition, origin: "platform-profile" as const, applyPath: "platform-profile" as const };
    const client = {
      listPluginConfigurationDefinitions: vi.fn(async () => [platformDefinition]),
      listPluginConfigurationCandidates: vi.fn(async () => [{
        id: "pcfg_x", configurationId: platformDefinition.id, revision: 4, status: "Publishing", applyPath: "platform-profile",
        catalogDigest: platformDefinition.catalogDigest, schemaDigest: platformDefinition.schemaDigest, artifactSha256: platformDefinition.artifact.sha256,
        values: platformDefinition.values, createdBy: "alice", createdAt: "2026-07-23T00:00:00Z", updatedAt: "2026-07-23T00:00:00Z", externalStatus: "PendingApproval",
      }]),
      submitPlatformProfileConfigurationDraft: submit,
      approvePlatformProfileConfigurationCandidate: approve,
      activatePlatformProfileConfigurationCandidate: activate,
      abortPlatformProfileConfigurationCandidate: abort,
    } as unknown as PlatformAdminClient;
    const page = createPluginConfigurationPage(client, "configuration", "/settings/plugin-configurations", message("test", "title", "Plugin configuration"));
    const result = await page.load({ mode: "page", page: 1, pageSize: 20, filters: {} }, new AbortController().signal);
    const selected = result.items;
    for (const id of ["submit-profile", "approve-profile", "activate-profile", "abort-profile"]) {
      await page.runAction?.({ action: { id, label: id, placement: "record.row" }, selected, refresh() {} }, new AbortController().signal);
    }
    expect(submit).toHaveBeenCalledWith("pcfg_x", 4);
    expect(approve).toHaveBeenCalledWith("pcfg_x", 4);
    expect(activate).toHaveBeenCalledWith("pcfg_x", 4);
    expect(abort).toHaveBeenCalledWith("pcfg_x", 4);
    expect((page.collection.actions ?? []).filter((action) => action.id.includes("profile")).every((action) => action.requiredPermissions?.includes("platform.plugin-configuration.profile.publish"))).toBe(true);
  });

  it("routes service hot actions only for a definition with a configuration controller", async () => {
    const submit = vi.fn(), approve = vi.fn(), activate = vi.fn(), abort = vi.fn();
    const hotDefinition = { ...definition, applyMode: "hot" as const, applyPath: "hot-service" as const, controllerAvailable: true };
    const client = {
      listPluginConfigurationDefinitions: vi.fn(async () => [hotDefinition]),
      listPluginConfigurationCandidates: vi.fn(async () => [{
        id: "pcfg_hot", configurationId: hotDefinition.id, revision: 5, status: "Publishing", applyPath: "hot-service",
        catalogDigest: hotDefinition.catalogDigest, schemaDigest: hotDefinition.schemaDigest, artifactSha256: hotDefinition.artifact.sha256,
        values: hotDefinition.values, createdBy: "alice", createdAt: "2026-07-23T00:00:00Z", updatedAt: "2026-07-23T00:00:00Z", externalStatus: "PendingApproval",
      }]),
      submitHotServiceConfigurationDraft: submit,
      approveHotServiceConfigurationCandidate: approve,
      activateHotServiceConfigurationCandidate: activate,
      abortHotServiceConfigurationCandidate: abort,
    } as unknown as PlatformAdminClient;
    const page = createPluginConfigurationPage(client, "configuration", "/settings/plugin-configurations", message("test", "title", "Plugin configuration"));
    const result = await page.load({ mode: "page", page: 1, pageSize: 20, filters: {} }, new AbortController().signal);
    for (const id of ["submit-hot", "approve-hot", "activate-hot", "abort-hot"]) {
      await page.runAction?.({ action: { id, label: id, placement: "record.row" }, selected: result.items, refresh() {} }, new AbortController().signal);
    }
    expect(submit).toHaveBeenCalledWith("pcfg_hot", 5);
    expect(approve).toHaveBeenCalledWith("pcfg_hot", 5);
    expect(activate).toHaveBeenCalledWith("pcfg_hot", 5);
    expect(abort).toHaveBeenCalledWith("pcfg_hot", 5);
    expect((page.collection.actions ?? []).filter((action) => action.id.includes("hot")).every((action) => action.requiredPermissions?.includes("platform.plugin-configuration.hot.publish"))).toBe(true);
  });
});
