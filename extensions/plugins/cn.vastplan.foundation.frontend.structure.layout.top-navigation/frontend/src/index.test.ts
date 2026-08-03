import { describe, expect, it } from "vitest";
import { uiContractVersion } from "@vastplan/ui-contract";
import { composedNavigationMenuItems, type PortalNavigationGroup, type UIShellProps } from "@vastplan/ui-primitives";
import adapter, { prioritizeRoots, topNavigationCapacity, topNavigationShellCSS } from "./index";

const root = (id: string): PortalNavigationGroup => ({ id, label: id, zone: "primary", icon: { kind: "semantic", name: "menu" }, pages: [], children: [] });

describe("top navigation shell layout", () => {
  it("exports an independent signed Shell Library", () => {
    expect(adapter).toMatchObject({ id: "top-navigation", shell: "ui.structure.shell", uiContract: uiContractVersion });
  });

  it("keeps the active root visible when navigation overflows", () => {
    const result = prioritizeRoots([root("one"), root("two"), root("three"), root("four")], 3, "four");
    expect(result.visible.map((item) => item.id)).toEqual(["one", "four"]);
    expect(result.overflow.map((item) => item.id)).toEqual(["two", "three"]);
  });

  it("sizes icon-only root navigation by the shared touch target instead of a text-menu estimate", () => {
    expect(topNavigationCapacity(294, 44)).toBe(6);
    expect(topNavigationCapacity(44, 44)).toBe(1);
  });

  it("uses the renderer-owned compact navigation Menu and a fixed page header", () => {
    expect(topNavigationShellCSS).toContain(".vp-top-navigation-menu-empty{min-width:220px;padding:8px");
    expect(topNavigationShellCSS).not.toContain(".vp-top-navigation-menu-link:hover");
    expect(topNavigationShellCSS).toContain(".vp-top-page-scroller{flex:1;min-height:0;overflow:auto");
    expect(topNavigationShellCSS).toContain("@media (max-width:767px)");
  });

  it("maps root, direct and nested pages into one Menu tree", () => {
    const page = (id: string) => ({ id, label: id, zone: "secondary" as const, groupID: "vastplan.host/account", parentMenuRef: { pluginID: "vastplan.host", nodeID: "account" } });
    const group: PortalNavigationGroup = { ...root("vastplan.host/account"), pages: [page("profile")], children: [{ ...root("preferences"), parentID: "vastplan.host/account", pages: [page("appearance")] }] };
    const composition = { pages: [{ id: "profile-page", path: "/profile", navigation: { id: "profile" } }, { id: "appearance-page", path: "/appearance", navigation: { id: "appearance" } }] } as unknown as UIShellProps["composition"];
    const items = composedNavigationMenuItems([group], composition, { locale: "zh-CN", text: (value) => typeof value === "string" ? value : value.fallback }, false);
    expect(items).toMatchObject([{ id: "profile", href: "/profile" }, { id: "group:preferences", children: [{ id: "appearance", href: "/appearance" }] }]);
  });

  it("places desktop page context before the main menu with an explicit visual boundary", () => {
    expect(topNavigationShellCSS).toContain(".vp-top-inline-page-header{box-sizing:border-box;display:grid");
    expect(topNavigationShellCSS).toContain(".vp-top-logo-page-divider,.vp-top-page-navigation-divider{align-self:center;width:1px;height:32px;flex:0 0 1px;margin:0 12px");
    expect(topNavigationShellCSS).toContain(".vp-top-account{display:flex;align-items:center;margin-left:12px;padding-left:12px");
    expect(topNavigationShellCSS).toContain(".vp-top-page-header{display:none}");
    expect(topNavigationShellCSS).toContain(".vp-top-inline-page-header{box-sizing:border-box;display:grid;grid-template-columns:minmax(0,1fr) auto minmax(0,1fr);align-items:center;gap:12px;flex:0 1 320px");
    expect(topNavigationShellCSS).toContain(".vp-top-center{flex:1 1 0;justify-content:flex-start;overflow:hidden}");
  });

  it("uses the renderer surface token for the page body", () => {
    expect(topNavigationShellCSS).toContain(".vp-top-page-scroller{flex:1;min-height:0;overflow:auto;overscroll-behavior:contain;background:var(--vp-top-surface)}");
    expect(topNavigationShellCSS).toContain("padding:var(--vp-page-content-start) 24px 24px");
  });

  it("leaves navigation row hover behavior to the renderer Menu", () => {
    expect(topNavigationShellCSS).not.toContain(".vp-top-mega");
    expect(topNavigationShellCSS).not.toContain(".vp-top-navigation-menu-group");
  });

  it("adds logout to the account data model consumed by the shared compact Menu", () => {
    const group: PortalNavigationGroup = { ...root("vastplan.host/account"), pages: [{ id: "profile", label: "profile", zone: "secondary", groupID: "vastplan.host/account", parentMenuRef: { pluginID: "vastplan.host", nodeID: "account" } }] };
    const composition = { pages: [{ id: "profile-page", path: "/profile", navigation: { id: "profile" } }] } as unknown as UIShellProps["composition"];
    const items = composedNavigationMenuItems([group], composition, { locale: "zh-CN", text: (value) => typeof value === "string" ? value : value.fallback }, true);
    expect(items).toMatchObject([{ id: "profile", href: "/profile" }, { id: "account.logout" }]);
  });
});
