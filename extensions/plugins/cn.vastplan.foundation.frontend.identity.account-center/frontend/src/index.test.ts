import { describe, expect, it, vi } from "vitest";
import accountCenter from "./index";

describe("account center plugin", () => {
  it("registers account pages through the standard Portal page contract", () => {
    const addPage = vi.fn();
    accountCenter.register({
      addPage,
      i18n: { message: (_key: string, fallback: string) => fallback },
    } as never);
    expect(addPage).toHaveBeenCalledTimes(2);
    expect(addPage.mock.calls.map(([page]) => page.navigation)).toEqual([
      expect.objectContaining({ id: "account.profile", groupID: "account", zone: "secondary" }),
      expect.objectContaining({ id: "account.settings.appearance", groupID: "account.settings", zone: "secondary" }),
    ]);
  });
});
