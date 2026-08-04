import { describe, expect, it, vi } from "vitest";
import type { PlatformAdminClient } from "@vastplan/platform-admin";
import { message } from "@vastplan/workbench-sdk";
import { createDatabaseConnectionsPage } from "./index.js";

describe("database connections Workbench page", () => {
  it("requires one-time material on create but never loads it for edit", async () => {
    const putDatabaseConnection = vi.fn(async () => ({}));
    const client = { putDatabaseConnection, listDatabaseConnections: vi.fn(async () => []) } as unknown as PlatformAdminClient;
    const page = createDatabaseConnectionsPage(client, "database", "/settings/databases", message("test", "title", "Databases"));
    const create = page.forms?.find((form) => form.id === "create");
    const edit = page.forms?.find((form) => form.id === "edit");
    const properties = create?.schema.schema.properties as Readonly<Record<string, Readonly<Record<string, unknown>>>> | undefined;
    expect(create?.presentation?.fields).toContainEqual(expect.objectContaining({ pointer: "/credentialValue", widget: "secretMaterial" }));
    expect(properties?.options).not.toHaveProperty("title");
    expect(properties?.pool).not.toHaveProperty("title");
    expect(create?.presentation?.sections).toEqual(expect.arrayContaining([
      expect.objectContaining({ id: "options", title: expect.objectContaining({ fallback: "Provider 连接选项" }) }),
      expect.objectContaining({ id: "pool", title: expect.objectContaining({ fallback: "连接池策略" }) }),
    ]));
    await expect(create?.validate?.({ value: {}, context: {}, signal: new AbortController().signal })).resolves.toEqual({ credentialValue: expect.objectContaining({ key: "error.credentialRequired" }) });
    const loaded = await edit?.load?.([{ name: "main", resourceId: "r", revision: 1, providerId: "postgresql", endpoint: "db:5432", options: { user: "app" }, pool: { maxIdle: 8, maxOpen: 32, maxLifetimeMs: 1000, maxIdleTimeMs: 1000, acquireTimeoutMs: 100, idlePoolTtlMs: 1000 }, runtime: "ready", credential: { managed: true, version: 2 }, credentialState: "managed", credentialVersion: 2 }], new AbortController().signal);
    expect(loaded).not.toHaveProperty("credentialValue");
    await create?.submit({ value: { name: "main", providerId: "postgresql", endpoint: "db:5432", options: { user: "app" }, credentialValue: "one-time" }, selected: [] }, new AbortController().signal);
    expect(putDatabaseConnection).toHaveBeenCalledWith("main", expect.objectContaining({ credentialValue: "one-time" }));
  });
});
