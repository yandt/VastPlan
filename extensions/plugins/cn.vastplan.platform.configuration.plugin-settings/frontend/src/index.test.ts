import { describe, expect, it, vi } from "vitest";
import type { PlatformAdminClient, PluginConfigurationDefinition } from "@vastplan/platform-admin";
import { message } from "@vastplan/workbench-sdk";
import { createPluginConfigurationPage } from "./index";

const definition: PluginConfigurationDefinition = {
  id: "cfg_aaaaaaaaaaaaaaaaaaaaaaaa", deployment: "agent-services", unitId: "api", pluginId: "com.example.configured", pluginName: "Configured",
  origin: "application", artifact: { version: "1.0.0", channel: "stable", sha256: "a".repeat(64) }, scope: "service", applyMode: "restart", applyPath: "application-deployment",
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
    expect(prepared?.schema?.schema).toEqual(definition.schema);
    await form?.submit({ value: { region: "cn-west" }, selected: result.items }, new AbortController().signal);
    expect(createDraft).toHaveBeenCalledWith(definition.id, definition.catalogDigest, { region: "cn-west" });
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
});
