import { describe, expect, it, vi } from "vitest";
import type { PlatformAdminClient, Setting } from "@vastplan/platform-admin";
import { message } from "@vastplan/workbench-sdk";
import { createGlobalSettingsPage } from "./index.js";

describe("global settings Workbench page", () => {
  it("uses governed collection forms and preserves optimistic versions", async () => {
    const settings: Setting[] = [{ key: "portal.locale", value: { default: "zh-CN" }, version: 7, updatedAt: "2026-07-21T00:00:00Z" }];
    const putSetting = vi.fn(async () => undefined);
    const deleteSetting = vi.fn(async () => undefined);
    const client = { listSettings: vi.fn(async () => settings), putSetting, deleteSetting } as unknown as PlatformAdminClient;
    const page = createGlobalSettingsPage(client, "settings", "/settings/global", message("test", "title", "Settings"));
    expect(page.collection.actions?.map((action) => [action.id, action.form])).toEqual([["create", "create"], ["edit", "edit"], ["delete", undefined]]);
    await expect(page.load({ mode: "page", page: 1, pageSize: 20, filters: { key: "portal" } }, new AbortController().signal)).resolves.toMatchObject({ total: 1 });
    const edit = page.forms?.find((form) => form.id === "edit")!;
    await edit.submit({ value: { key: "portal.locale", value: "{\"default\":\"en-US\"}" }, selected: settings as Array<Setting & Record<string, unknown>> }, new AbortController().signal);
    expect(putSetting).toHaveBeenCalledWith("portal.locale", { default: "en-US" }, 7);
    await page.runAction?.({ action: page.collection.actions!.find((action) => action.id === "delete")!, selected: settings as Array<Setting & Record<string, unknown>>, refresh() {} }, new AbortController().signal);
    expect(deleteSetting).toHaveBeenCalledWith("portal.locale", 7);
  });

  it("keeps invalid JSON inside the field error channel", async () => {
    const client = { listSettings: async () => [], putSetting: vi.fn() } as unknown as PlatformAdminClient;
    const page = createGlobalSettingsPage(client, "settings", "/settings/global", message("test", "title", "Settings"));
    const create = page.forms?.find((form) => form.id === "create")!;
    await expect(create.validate?.({ value: { value: "{" }, context: {}, signal: new AbortController().signal })).resolves.toEqual({ value: expect.objectContaining({ key: "error.valueInvalid" }) });
    await expect(create.submit({ value: { key: "portal.locale", value: "{" }, selected: [] }, new AbortController().signal)).resolves.toEqual({ fieldErrors: { value: expect.objectContaining({ key: "error.valueInvalid" }) } });
  });
});
