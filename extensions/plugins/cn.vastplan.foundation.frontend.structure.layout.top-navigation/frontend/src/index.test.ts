import { describe, expect, it } from "vitest";
import { uiContractVersion } from "@vastplan/ui-contract";
import adapter, { navigationMenuItems, prioritizeRoots, topNavigationShellCSS } from "./index";
import type { PortalNavigationGroup, UIShellProps } from "@vastplan/ui-primitives";

const root = (id: string): PortalNavigationGroup => ({ id, label: id, zone: "primary", icon: "menu", pages: [], children: [] });

describe("top navigation shell layout", () => {
  it("exports an independent signed Shell Library", () => {
    expect(adapter).toMatchObject({ id: "top-navigation", shell: "ui.structure.shell", uiContract: uiContractVersion });
  });

  it("keeps the active root visible when navigation overflows", () => {
    const result = prioritizeRoots([root("one"), root("two"), root("three"), root("four")], 3, "four");
    expect(result.visible.map((item) => item.id)).toEqual(["one", "four"]);
    expect(result.overflow.map((item) => item.id)).toEqual(["two", "three"]);
  });

  it("uses the renderer-owned compact navigation Menu and a fixed page header", () => {
    expect(topNavigationShellCSS).toContain(".vp-top-navigation-menu-empty{min-width:220px;padding:8px");
    expect(topNavigationShellCSS).not.toContain(".vp-top-navigation-menu-link:hover");
    expect(topNavigationShellCSS).toContain(".vp-top-page-scroller{flex:1;min-height:0;overflow:auto");
    expect(topNavigationShellCSS).toContain("@media (max-width:767px)");
  });

  it("maps root, direct and nested pages into one Menu tree", () => {
    const group: PortalNavigationGroup = { ...root("account"), pages: [{ id: "profile", label: "Profile", zone: "secondary" }], children: [{ ...root("preferences"), parentID: "account", pages: [{ id: "appearance", label: "Appearance", zone: "secondary" }] }] };
    const composition = { pages: [{ id: "profile-page", path: "/profile", navigation: { id: "profile" } }, { id: "appearance-page", path: "/appearance", navigation: { id: "appearance" } }] } as unknown as UIShellProps["composition"];
    const items = navigationMenuItems([group], composition, { text: (value) => typeof value === "string" ? value : value.fallback });
    expect(items).toMatchObject([{ id: "profile", href: "/profile" }, { id: "group:preferences", children: [{ id: "appearance", href: "/appearance" }] }]);
  });

  it("places desktop page context before the main menu with an explicit visual boundary", () => {
    expect(topNavigationShellCSS).toContain(".vp-top-inline-page-header{box-sizing:border-box;display:grid");
    expect(topNavigationShellCSS).toContain(".vp-top-logo-page-divider,.vp-top-page-navigation-divider{align-self:center;width:1px;height:32px;flex:0 0 1px;margin:0 12px");
    expect(topNavigationShellCSS).toContain(".vp-top-account{display:flex;align-items:center;margin-left:12px;padding-left:12px");
    expect(topNavigationShellCSS).toContain(".vp-top-page-header{display:none}");
  });

  it("uses the renderer surface token for the page body", () => {
    expect(topNavigationShellCSS).toContain(".vp-top-page-scroller{flex:1;min-height:0;overflow:auto;overscroll-behavior:contain;background:var(--vp-top-surface)}");
    expect(topNavigationShellCSS).toContain("padding:var(--vp-page-content-start) 24px 24px");
  });

  it("leaves navigation row hover behavior to the renderer Menu", () => {
    expect(topNavigationShellCSS).not.toContain(".vp-top-mega");
    expect(topNavigationShellCSS).not.toContain(".vp-top-navigation-menu-group");
  });
});
