import { describe, expect, it, vi } from "vitest";
import { PlatformAdminError, type PlatformAdminClient } from "@vastplan/platform-admin";
import { message } from "@vastplan/workbench-sdk";
import databaseConnectionsPlugin, { createDatabaseConnectionsPage } from "./index.js";
import { createPlatformControlPage } from "./platform-control-page.js";

describe("database connections Workbench page", () => {
  it("keeps Platform Control bootstrap configuration data-driven and secret-reference only", async () => {
    const testPlatformControl = vi.fn(async () => ({ phase: "unconfigured" as const }));
    const configurePlatformControl = vi.fn(async () => ({ phase: "ready" as const, generation: 1 }));
    const client = {
      platformControlStatus: vi.fn(async () => ({ phase: "unconfigured" as const })),
      testPlatformControl,
      configurePlatformControl,
    } as unknown as PlatformAdminClient;
    const page = createPlatformControlPage(client, "database", "/settings/databases/platform-control");
    expect(page.form.workflow).toMatchObject({ surface: "page", submitLabel: expect.objectContaining({ key: "platformControl.action.initialize" }) });
    expect(page.form.workflow.actions).toContainEqual(expect.objectContaining({ id: "test", placement: "footer.start", requiresValid: true }));
    const loaded = await page.form.load?.([], new AbortController().signal);
    expect(loaded).toMatchObject({ phase: "unconfigured", currentGeneration: 0, providerId: "postgresql", secretKind: "systemd-credential", contractRange: "^1.0.0" });
    const value = { ...loaded, host: "db.internal", username: "vastplan", serverName: "db.internal", secretName: "platform-control-db" };
    const action = page.form.workflow.actions?.[0];
    if (action === undefined) throw new Error("平台控制数据库测试动作未注册");
    await page.form.runAction?.({ action, value, selected: [] }, new AbortController().signal);
    await page.form.submit({ value, selected: [] }, new AbortController().signal);
    const expected = expect.objectContaining({ expectedGeneration: 0, profile: expect.objectContaining({ schemaVersion: 1, generation: 1, endpoint: "db.internal:5432", secretRef: { kind: "systemd-credential", name: "platform-control-db" } }) });
    expect(testPlatformControl).toHaveBeenCalledWith(expected);
    expect(configurePlatformControl).toHaveBeenCalledWith(expected);
    expect(JSON.stringify(testPlatformControl.mock.calls)).not.toContain("password");
  });

  it("requires one-time material on create but never loads it for edit", async () => {
    const putDatabaseConnection = vi.fn(async () => ({}));
    const testDatabaseConnection = vi.fn(async () => ({ ready: true, providerId: "postgresql", latencyMs: 12 }));
    const client = { putDatabaseConnection, testDatabaseConnection, listDatabaseConnections: vi.fn(async () => []) } as unknown as PlatformAdminClient;
    const page = createDatabaseConnectionsPage(client, "database", "/settings/databases", message("test", "title", "Databases"));
    const create = page.forms?.find((form) => form.id === "create");
    const edit = page.forms?.find((form) => form.id === "edit");
    const properties = create?.schema.schema.properties as Readonly<Record<string, Readonly<Record<string, unknown>>>> | undefined;
    const editProperties = edit?.schema.schema.properties as Readonly<Record<string, Readonly<Record<string, unknown>>>> | undefined;
    expect(create?.presentation).toMatchObject({ layout: "horizontal", labelPlacement: "inline" });
    expect(create?.schema.uiSchema).toMatchObject({ options: {
      user: { "ui:options": { autocomplete: "new-password" } },
      password: { "ui:options": { autocomplete: "new-password" } },
    } });
    expect(properties?.host).toMatchObject({ title: "地址", minLength: 1 });
    expect(properties?.port).toMatchObject({ title: "端口", minimum: 1, maximum: 65535 });
    expect(properties).not.toHaveProperty("endpoint");
    const optionProperties = properties?.options?.properties as Readonly<Record<string, Readonly<Record<string, unknown>>>> | undefined;
    expect(create?.presentation?.fields).toContainEqual(expect.objectContaining({ pointer: "/options/password", widget: "secretMaterial" }));
    expect(Object.keys(optionProperties ?? {}).slice(0, 2)).toEqual(["user", "password"]);
    expect(optionProperties?.password).toMatchObject({ title: "密码", format: "vastplan-secret-material", writeOnly: true });
    expect(properties?.options?.required).toEqual(["user", "password"]);
    expect(editProperties?.options?.required).toEqual(["user"]);
    expect(create?.presentation?.fields?.find((field) => field.pointer === "/options/password")).not.toHaveProperty("help");
    expect(properties?.options).not.toHaveProperty("title");
    expect(properties?.pool).not.toHaveProperty("title");
    expect(create?.presentation?.sections).toEqual(expect.arrayContaining([
      expect.objectContaining({ id: "options", title: expect.objectContaining({ fallback: "连接选项" }), columns: 2, fields: ["/options"] }),
      expect.objectContaining({ id: "pool", title: expect.objectContaining({ fallback: "连接池策略" }) }),
    ]));
    expect(create?.presentation?.sections).not.toEqual(expect.arrayContaining([expect.objectContaining({ id: "credential" })]));
    expect(create?.workflow.actions).toContainEqual(expect.objectContaining({ id: "test", placement: "footer.start", icon: "success", requiresValid: true }));
    const testAction = create?.workflow.actions?.[0];
    if (testAction === undefined) throw new Error("测试连接 Form Action 未注册");
    await expect(create?.validate?.({ value: { options: { user: "app" } }, context: {}, signal: new AbortController().signal })).resolves.toMatchObject({
      "/options/password": expect.objectContaining({ key: "error.credentialRequired" }),
      "/host": expect.objectContaining({ key: "error.hostInvalid" }),
      "/port": expect.objectContaining({ key: "error.portInvalid" }),
    });
    await expect(create?.validate?.({ value: { name: "main", providerId: "postgresql", host: "db", port: 5432, options: { password: "one-time" } }, context: {}, signal: new AbortController().signal })).resolves.toMatchObject({
      "/options/user": expect.objectContaining({ key: "error.userRequired" }),
    });
    const loaded = await edit?.load?.([{ name: "main", resourceId: "r", revision: 1, providerId: "postgresql", endpoint: "db:5432", options: { user: "app" }, pool: { maxIdle: 8, maxOpen: 32, maxLifetimeMs: 1000, maxIdleTimeMs: 1000, acquireTimeoutMs: 100, idlePoolTtlMs: 1000 }, runtime: "ready", credential: { managed: true, version: 2 }, credentialState: "managed", credentialVersion: 2 }], new AbortController().signal);
    expect(loaded?.options).not.toHaveProperty("password");
    expect(loaded).toMatchObject({ host: "db", port: 5432 });
    await create?.submit({ value: { name: "main", providerId: "postgresql", host: "db", port: 5432, options: { user: "app", password: "one-time", network: "tcp", readTimeoutMs: 500 } }, selected: [] }, new AbortController().signal);
    expect(putDatabaseConnection).toHaveBeenCalledWith("main", expect.objectContaining({ endpoint: "db:5432", options: { user: "app" }, credentialValue: "one-time" }));
    const outcome = await create?.runAction?.({ action: testAction, value: { name: "main", providerId: "postgresql", host: "db", port: 5432, options: { user: "app", password: "one-time" } }, selected: [] }, new AbortController().signal);
    expect(testDatabaseConnection).toHaveBeenCalledWith("main", expect.objectContaining({ endpoint: "db:5432", options: { user: "app" }, credentialValue: "one-time" }));
    expect(outcome?.notify).toMatchObject({ kind: "success", title: expect.objectContaining({ key: "test.success" }), content: expect.objectContaining({ values: { provider: "PostgreSQL", latency: 12 } }) });
  });

  it("uses Chinese functional labels in the Chinese locale", () => {
    const messages = databaseConnectionsPlugin.localization.messages["zh-CN"];
    expect(messages).toMatchObject({
      "form.provider": "数据库类型",
      "form.socketPath": "Unix 套接字路径",
      "form.credential": "密码",
      "form.tlsMode": "传输加密模式",
      "form.serverName": "证书校验服务器名称",
      "form.applicationName": "客户端应用名称",
      "section.options": "连接选项",
      "filter.provider": "数据库类型",
      "column.provider": "数据库类型",
      "column.runtime": "运行状态",
      "action.test": "测试连接",
      "test.success": "连接测试成功",
    });
  });

  it("renders a localized actionable diagnosis for connection-test failures", async () => {
    const testDatabaseConnection = vi.fn(async () => { throw new PlatformAdminError(422, "database_authentication_failed"); });
    const client = { testDatabaseConnection, listDatabaseConnections: vi.fn(async () => []) } as unknown as PlatformAdminClient;
    const page = createDatabaseConnectionsPage(client, "database", "/settings/databases", message("test", "title", "Databases"));
    const create = page.forms?.find((form) => form.id === "create");
    const testAction = create?.workflow.actions?.find((action) => action.id === "test");
    if (testAction === undefined) throw new Error("测试连接 Form Action 未注册");
    const outcome = await create?.runAction?.({ action: testAction, value: { name: "main", providerId: "postgresql", host: "db", port: 5432, options: { user: "app", password: "wrong" } }, selected: [] }, new AbortController().signal);
    expect(outcome?.notify).toMatchObject({ kind: "error", content: expect.objectContaining({ key: "test.authenticationDetail" }) });
  });

  it("asks the user to replace an unusable managed database password", async () => {
    const testDatabaseConnection = vi.fn(async () => { throw new PlatformAdminError(422, "database_credential_unavailable"); });
    const client = { testDatabaseConnection, listDatabaseConnections: vi.fn(async () => []) } as unknown as PlatformAdminClient;
    const page = createDatabaseConnectionsPage(client, "database", "/settings/databases", message("test", "title", "Databases"));
    const edit = page.forms?.find((form) => form.id === "edit");
    const testAction = edit?.workflow.actions?.find((action) => action.id === "test");
    if (testAction === undefined) throw new Error("测试连接 Form Action 未注册");
    const outcome = await edit?.runAction?.({ action: testAction, value: { name: "main", providerId: "postgresql", host: "db", port: 5432, options: { user: "app" } }, selected: [] }, new AbortController().signal);
    expect(outcome?.notify).toMatchObject({ kind: "error", content: expect.objectContaining({ key: "test.credentialDetail" }) });
  });

  it("preserves MySQL Unix socket endpoints without inventing a TCP port", async () => {
    const putDatabaseConnection = vi.fn(async () => ({}));
    const client = { putDatabaseConnection, listDatabaseConnections: vi.fn(async () => []) } as unknown as PlatformAdminClient;
    const page = createDatabaseConnectionsPage(client, "database", "/settings/databases", message("test", "title", "Databases"));
    const create = page.forms?.find((form) => form.id === "create");
    await create?.submit({ value: { name: "local", providerId: "mysql", socketPath: "/var/run/mysql.sock", options: { user: "app", password: "one-time", network: "unix" } }, selected: [] }, new AbortController().signal);
    expect(putDatabaseConnection).toHaveBeenCalledWith("local", expect.objectContaining({ endpoint: "/var/run/mysql.sock" }));
  });
});
