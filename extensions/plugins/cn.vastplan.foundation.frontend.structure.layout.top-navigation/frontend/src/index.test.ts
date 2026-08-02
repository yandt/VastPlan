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

  it("uses one bounded mega popover and a fixed page header", () => {
    expect(topNavigationShellCSS).toContain("--vp-top-mega-min");
    expect(topNavigationShellCSS).toContain("grid-template-columns:repeat(auto-fit,minmax(220px,1fr))");
    expect(topNavigationShellCSS).toContain(".vp-top-page-scroller{flex:1;min-height:0;overflow:auto");
    expect(topNavigationShellCSS).toContain("@media (max-width:767px)");
  });

  it("places desktop page context before the main menu with a visual boundary", () => {
    expect(topNavigationShellCSS).toContain(".vp-top-inline-page-header{box-sizing:border-box;display:grid");
    expect(topNavigationShellCSS).toContain(".vp-top-inline-page-header+.vp-top-center");
    expect(topNavigationShellCSS).toContain("padding-left:12px;border-left:1px solid var(--vp-top-border)");
    expect(topNavigationShellCSS).toContain(".vp-top-page-header{display:none}");
  });

  it("uses the renderer surface token for the page body", () => {
    expect(topNavigationShellCSS).toContain(".vp-top-page-scroller{flex:1;min-height:0;overflow:auto;overscroll-behavior:contain;background:var(--vp-top-surface)}");
    expect(topNavigationShellCSS).toContain("padding:var(--vp-page-content-start) 24px 24px");
  });

  it("keeps direct and nested second-level entries in one visual rhythm", () => {
    expect(topNavigationShellCSS).toContain(".vp-top-direct-pages,.vp-top-child-grid{display:grid");
    expect(topNavigationShellCSS).toContain(".vp-top-child-group h3{box-sizing:border-box;display:flex;align-items:center;min-height:var(--vp-top-touch-minimum)");
    expect(topNavigationShellCSS).not.toContain(".vp-top-direct-pages{display:flex");
    expect(topNavigationShellCSS).not.toContain(".vp-top-direct-pages{display:grid;grid-template-columns:repeat(auto-fit,minmax(220px,1fr));gap:8px;padding-bottom:12px;border-bottom");
  });
});
