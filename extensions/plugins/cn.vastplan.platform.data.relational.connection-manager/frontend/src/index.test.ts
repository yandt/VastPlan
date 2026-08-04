import { describe, expect, it, vi } from "vitest";
import type { PlatformAdminClient } from "@vastplan/platform-admin";
import { message } from "@vastplan/workbench-sdk";
import databaseConnectionsPlugin, { createDatabaseConnectionsPage } from "./index.js";

describe("database connections Workbench page", () => {
  it("requires one-time material on create but never loads it for edit", async () => {
    const putDatabaseConnection = vi.fn(async () => ({}));
    const client = { putDatabaseConnection, listDatabaseConnections: vi.fn(async () => []) } as unknown as PlatformAdminClient;
    const page = createDatabaseConnectionsPage(client, "database", "/settings/databases", message("test", "title", "Databases"));
    const create = page.forms?.find((form) => form.id === "create");
    const edit = page.forms?.find((form) => form.id === "edit");
    const properties = create?.schema.schema.properties as Readonly<Record<string, Readonly<Record<string, unknown>>>> | undefined;
    expect(create?.presentation).toMatchObject({ layout: "horizontal", labelPlacement: "inline" });
    const optionProperties = properties?.options?.properties as Readonly<Record<string, Readonly<Record<string, unknown>>>> | undefined;
    expect(create?.presentation?.fields).toContainEqual(expect.objectContaining({ pointer: "/options/password", widget: "secretMaterial" }));
    expect(Object.keys(optionProperties ?? {}).slice(0, 2)).toEqual(["user", "password"]);
    expect(optionProperties?.password).toMatchObject({ title: "密码", format: "vastplan-secret-material", writeOnly: true });
    expect(properties?.options).not.toHaveProperty("title");
    expect(properties?.pool).not.toHaveProperty("title");
    expect(create?.presentation?.sections).toEqual(expect.arrayContaining([
      expect.objectContaining({ id: "options", title: expect.objectContaining({ fallback: "连接选项" }), columns: 2, fields: ["/options"] }),
      expect.objectContaining({ id: "pool", title: expect.objectContaining({ fallback: "连接池策略" }) }),
    ]));
    expect(create?.presentation?.sections).not.toEqual(expect.arrayContaining([expect.objectContaining({ id: "credential" })]));
    await expect(create?.validate?.({ value: { options: { user: "app" } }, context: {}, signal: new AbortController().signal })).resolves.toEqual({ "/options/password": expect.objectContaining({ key: "error.credentialRequired" }) });
    const loaded = await edit?.load?.([{ name: "main", resourceId: "r", revision: 1, providerId: "postgresql", endpoint: "db:5432", options: { user: "app" }, pool: { maxIdle: 8, maxOpen: 32, maxLifetimeMs: 1000, maxIdleTimeMs: 1000, acquireTimeoutMs: 100, idlePoolTtlMs: 1000 }, runtime: "ready", credential: { managed: true, version: 2 }, credentialState: "managed", credentialVersion: 2 }], new AbortController().signal);
    expect(loaded?.options).not.toHaveProperty("password");
    await create?.submit({ value: { name: "main", providerId: "postgresql", endpoint: "db:5432", options: { user: "app", password: "one-time" } }, selected: [] }, new AbortController().signal);
    expect(putDatabaseConnection).toHaveBeenCalledWith("main", expect.objectContaining({ options: { user: "app" }, credentialValue: "one-time" }));
  });

  it("uses Chinese functional labels in the Chinese locale", () => {
    const messages = databaseConnectionsPlugin.localization.messages["zh-CN"];
    expect(messages).toMatchObject({
      "form.provider": "数据库类型",
      "form.credential": "密码",
      "form.tlsMode": "传输加密模式",
      "form.serverName": "证书校验服务器名称",
      "form.applicationName": "客户端应用名称",
      "section.options": "连接选项",
      "filter.provider": "数据库类型",
      "column.provider": "数据库类型",
      "column.runtime": "运行状态",
    });
  });
});
