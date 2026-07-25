import { describe, expect, it } from "vitest";
import type { PlatformAdminClient } from "@vastplan/platform-admin";
import { permissionsPage, rolesPage, bindingsPage, auditPage } from "./index";

describe("role management workspaces",()=>{
  const client={} as PlatformAdminClient;
  it("registers four governed Workbench pages",()=>{
    const pages=[permissionsPage(client),rolesPage(client),bindingsPage(client),auditPage(client)];
    expect(pages.map(page=>page.path)).toEqual(["/settings/authorization/permissions","/settings/authorization/roles","/settings/authorization/bindings","/settings/authorization/audit"]);
    expect(pages.every(page=>page.collection.view==="table")).toBe(true);
  });
});
