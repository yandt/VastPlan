import { describe, expect, it, vi } from "vitest";
import type { PlatformAdminClient } from "@vastplan/platform-admin";
import { message } from "@vastplan/workbench-sdk";
import { createCredentialsPage } from "./index.js";

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
});
