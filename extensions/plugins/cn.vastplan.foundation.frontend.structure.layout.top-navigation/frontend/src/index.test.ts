import { describe, expect, it } from "vitest";
import { uiContractVersion } from "@vastplan/ui-contract";
import adapter, { prioritizeRoots, topNavigationShellCSS } from "./index";
import type { PortalNavigationGroup } from "@vastplan/ui-primitives";

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

  it("uses one compact navigation menu surface and a fixed page header", () => {
    expect(topNavigationShellCSS).toContain(".vp-top-navigation-menu{box-sizing:border-box;width:clamp(240px,28vw,400px)");
    expect(topNavigationShellCSS).toContain("padding:4px");
    expect(topNavigationShellCSS).toContain(".vp-top-navigation-menu-link:hover{background:var(--vp-top-selected)");
    expect(topNavigationShellCSS).toContain(".vp-top-page-scroller{flex:1;min-height:0;overflow:auto");
    expect(topNavigationShellCSS).toContain("@media (max-width:767px)");
  });

  it("places desktop page context before the main menu with an explicit visual boundary", () => {
    expect(topNavigationShellCSS).toContain(".vp-top-inline-page-header{box-sizing:border-box;display:grid");
    expect(topNavigationShellCSS).toContain(".vp-top-page-navigation-divider{align-self:center;width:1px;height:32px");
    expect(topNavigationShellCSS).toContain(".vp-top-page-header{display:none}");
  });

  it("uses the renderer surface token for the page body", () => {
    expect(topNavigationShellCSS).toContain(".vp-top-page-scroller{flex:1;min-height:0;overflow:auto;overscroll-behavior:contain;background:var(--vp-top-surface)}");
    expect(topNavigationShellCSS).toContain("padding:var(--vp-page-content-start) 24px 24px");
  });

  it("keeps direct and nested entries in one compact menu rhythm", () => {
    expect(topNavigationShellCSS).toContain(".vp-top-navigation-menu-group{display:grid;gap:2px");
    expect(topNavigationShellCSS).toContain(".vp-top-navigation-menu-child-group{display:grid;gap:2px");
    expect(topNavigationShellCSS).not.toContain(".vp-top-mega");
  });
});
