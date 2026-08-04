import { describe, expect, it, vi } from "vitest";
import type { PlatformAdminClient } from "@vastplan/platform-admin";
import { message } from "@vastplan/workbench-sdk";
import { createDatabaseConnectionsPage } from "./index.js";

describe("database connection form layout", () => {
  it("uses two-column sections while letting their direct object span the complete section", () => {
    const client = { listDatabaseConnections: vi.fn(async () => []) } as unknown as PlatformAdminClient;
    const page = createDatabaseConnectionsPage(client, "database", "/settings/databases", message("test", "title", "Databases"));
    const presentation = page.forms?.find((form) => form.id === "create")?.presentation;

    expect(presentation?.sections).toEqual(expect.arrayContaining([
      expect.objectContaining({ id: "identity", columns: 2 }),
      expect.objectContaining({ id: "options", columns: 2, fields: ["/options"] }),
      expect.objectContaining({ id: "pool", columns: 2 }),
    ]));
    expect(presentation?.sections).not.toEqual(expect.arrayContaining([expect.objectContaining({ id: "credential" })]));
    expect(presentation?.fields).toEqual(expect.arrayContaining([
      expect.objectContaining({ pointer: "/options", span: 2 }),
      expect.objectContaining({ pointer: "/pool", span: 2 }),
      expect.objectContaining({ pointer: "/options/password", widget: "secretMaterial" }),
      expect.objectContaining({ pointer: "/options/serverName", visibleWhen: { pointer: "/options/tlsMode", equals: "verify-full" } }),
    ]));
  });
});
