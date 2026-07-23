import { describe, expect, it, vi } from "vitest";
import type { PlatformAdminClient } from "@vastplan/platform-admin";
import { message } from "@vastplan/workbench-sdk";
import { createCredentialsPage } from "./index.js";
import { createCredentialAuditPage } from "./audit-page.js";

describe("credentials Workbench page", () => {
  it("uses a governed one-time secret field and never loads secret material", async () => {
    const putCredential = vi.fn(async () => ({ name: "db", version: 1 }));
    const client = { putCredential, listCredentials: vi.fn(async () => []) } as unknown as PlatformAdminClient;
    const page = createCredentialsPage(client, "credentials", "/settings/credentials", message("test", "title", "Credentials"));
    const form = page.forms?.[0];
    expect(form?.presentation?.fields).toContainEqual(expect.objectContaining({ pointer: "/value", widget: "secretMaterial" }));
    expect(form?.initialValue).toBeUndefined();
    await form?.submit({ value: { name: "db", value: "one-time" }, selected: [] }, new AbortController().signal);
    expect(putCredential).toHaveBeenCalledWith("db", "one-time");
  });

  it("renders only redacted managed lifecycle audit facts", async () => {
    const listManagedCredentialAudit = vi.fn(async () => ({
      items: [{ id: 1, credentialFingerprint: "a".repeat(32), action: "managed.auto-aborted", state: "Aborted", owner: "cn.example", purpose: "remote.token", resource: "resource", delegated: true, occurredAt: "2026-07-23T00:00:00Z" }],
      maintenance: { autoAborted: 1, collected: 0, counts: { Aborted: 1 } },
    }));
    const client = { listManagedCredentialAudit } as unknown as PlatformAdminClient;
    const page = createCredentialAuditPage(client, "credentials", "/settings/credentials-audit");
    const result = await page.load({ mode: "cursor", page: 1, pageSize: 50, filters: {} }, new AbortController().signal);
    expect(result.items).toHaveLength(1);
    expect(JSON.stringify(result)).not.toContain("credential://");
    expect(listManagedCredentialAudit).toHaveBeenCalledWith(undefined, 50);
    const summary = await page.loadSummary?.(new AbortController().signal);
    expect(summary?.metrics).toEqual(expect.arrayContaining([expect.objectContaining({ id: "autoAborted", value: 1 })]));
  });
});
