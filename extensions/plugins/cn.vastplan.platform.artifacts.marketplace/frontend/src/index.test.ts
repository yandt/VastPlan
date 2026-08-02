import { describe, expect, it, vi } from "vitest";
import type { PlatformAdminClient } from "@vastplan/platform-admin";
import { createMarketPage } from "./market-page.js";
import { createTaskPage } from "./task-page.js";

const message = (_key: string, fallback: string) => fallback;

describe("plugin marketplace Workbench pages", () => {
  it("merges configured markets without accepting a URL from page filters", async () => {
    const marketplace = {
      listPluginMarketplaceSources: vi.fn(async () => ({ version: 1, sources: [{ id: "platform", label: "平台市场", url: "vastplan://platform.artifacts.repository", priority: 0 }, { id: "enterprise", label: "企业市场", url: "https://plugins.example", priority: 10 }] })),
      listPluginMarketplaceCatalog: vi.fn(async ({ sourceId }: { sourceId: string }) => ({ version: 1, source: { id: sourceId }, revision: 1, total: 1, page: 1, pageSize: 100, items: [{ ref: { pluginId: `cn.example.${sourceId}`, version: "1.0.0", channel: "stable" }, sha256: "a".repeat(64), size: 1, publisher: "example", keyId: "key", signedAt: "2026-01-01T00:00:00Z", publishedAt: "2026-01-01T00:00:00Z", repositoryRevision: 1, name: sourceId, description: "", namespace: "cn.example", targets: ["backend"], lifecycleStatus: "active" }] })),
    } as unknown as PlatformAdminClient;
    const page = createMarketPage(marketplace, {} as PlatformAdminClient, "/market", message);
    const result = await page.load({ mode: "page", page: 1, pageSize: 20, filters: {} }, new AbortController().signal);
    expect(result.total).toBe(2);
    expect(marketplace.listPluginMarketplaceCatalog).toHaveBeenCalledTimes(2);
    expect(result.items).toEqual(expect.arrayContaining([
      expect.objectContaining({ sourceId: "platform", installable: true, availability: "installable" }),
      expect.objectContaining({ sourceId: "enterprise", installable: false, availability: "importRequired" }),
    ]));
    expect(page.collection.actions?.[0]).toMatchObject({ id: "install", form: "market-install", requiredPermissions: ["platform.deployment.plugin.request"], visibleWhen: { pointer: "/installable", equals: true } });
  });

  it("keeps self-service lifecycle operations data-driven", () => {
    const page = createTaskPage({} as PlatformAdminClient, "/tasks", message);
    const actions = Object.fromEntries((page.collection.actions ?? []).map((action) => [action.id, action]));
    expect(actions.approve).toMatchObject({ form: "approve-service-plugin-installation", requiredPermissions: ["platform.deployment.plugin.approve"] });
    expect(actions.activate.requiredPermissions).toEqual(["platform.deployment.plugin.activate"]);
  });

  it("leaves platform and foundation entries under platform baseline management", async () => {
    const marketplace = {
      listPluginMarketplaceSources: vi.fn(async () => ({ version: 1, sources: [{ id: "platform", label: "平台市场", url: "vastplan://platform.artifacts.repository", priority: 0 }] })),
      listPluginMarketplaceCatalog: vi.fn(async () => ({ version: 1, source: { id: "platform" }, revision: 1, total: 1, page: 1, pageSize: 100, items: [{ ref: { pluginId: "cn.vastplan.platform.configuration.settings", version: "1.0.0", channel: "stable" }, sha256: "a".repeat(64), size: 1, publisher: "vastplan", keyId: "key", signedAt: "2026-01-01T00:00:00Z", publishedAt: "2026-01-01T00:00:00Z", repositoryRevision: 1, name: "Settings", description: "", namespace: "cn.vastplan.platform", targets: ["backend"], lifecycleStatus: "active" }] })),
    } as unknown as PlatformAdminClient;
    const page = createMarketPage(marketplace, {} as PlatformAdminClient, "/market", message);
    const result = await page.load({ mode: "page", page: 1, pageSize: 20, filters: {} }, new AbortController().signal);
    expect(result.items[0]).toMatchObject({ installable: false, availability: "platformManaged" });
  });
});
