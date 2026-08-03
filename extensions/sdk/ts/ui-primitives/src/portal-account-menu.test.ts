import { describe, expect, it } from "vitest";
import { accountLogoutMenuItemID, accountMenuItems } from "./portal-account-menu";
import { accountNavigationNodeID } from "./portal-runtime";

describe("PortalAccountMenu", () => {
  it("keeps account pages in the composed tree and appends logout as a non-route action", () => {
    const group = {
      id: accountNavigationNodeID, label: "用户", zone: "secondary", icon: "info", order: 1,
      pages: [{ id: "account.profile", label: "用户信息", zone: "secondary" }],
      children: [{ id: "preferences", parentID: "account", label: "偏好设置", zone: "secondary", icon: "settings", order: 1, pages: [{ id: "account.appearance", label: "外观", zone: "secondary" }], children: [] }],
    } as never;
    const composition = {
      pages: [
        { id: "profile", path: "/account/profile", navigation: { id: "account.profile" } },
        { id: "appearance", path: "/account/settings/appearance", navigation: { id: "account.appearance" } },
      ],
    } as never;
    const items = accountMenuItems(group, composition, { text: (value: unknown) => typeof value === "string" ? value : (value as { fallback: string }).fallback }, true);
    expect(items).toMatchObject([
      { id: "account.profile", href: "/account/profile" },
      { id: "group:preferences", children: [{ id: "account.appearance", href: "/account/settings/appearance" }] },
      { id: accountLogoutMenuItemID },
    ]);
  });
});
