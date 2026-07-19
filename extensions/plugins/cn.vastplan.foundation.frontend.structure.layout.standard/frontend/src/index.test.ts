import { describe, expect, it } from "vitest";
import type { PortalSlotContribution, ShellCompositionModel } from "@vastplan/ui-primitives";
import adapter, { groups, standardShellCSS } from "./index";
import { hasRegionContent } from "./region-visibility";

function composition(overrides: Partial<ShellCompositionModel> = {}): ShellCompositionModel {
  return {
    pages: [],
    navigation: { primary: [], settings: [], secondary: [] },
    shellSlots: {},
    pageSlots: {},
    ...overrides,
  };
}

const contribution: PortalSlotContribution<"shell.header.start"> & { pluginID: string } = { id: "content", pluginID: "cn.vastplan.foundation.test", slot: "shell.header.start", component: () => null };

describe("standard shell layout", () => {
  it("exports only the visual layout adapter contract", () => {
    expect(adapter.id).toBe("internal.standard-template-source");
    expect(adapter.uiContract).toBe("4.0.0");
    expect(adapter.Shell).toBeTypeOf("function");
    expect(adapter).not.toHaveProperty("compose");
  });

  it("collapses a region with no slots, navigation or intrinsic layout content", () => {
    expect(hasRegionContent(composition(), { shellSlots: ["shell.header.start", "shell.header.center", "shell.header.end"] })).toBe(false);
    expect(hasRegionContent(composition({ pageSlots: { "page.aside": [] } }), { pageSlots: ["page.aside"] })).toBe(false);
  });

  it("keeps a region when any supported content source is present", () => {
    expect(hasRegionContent(composition({ shellSlots: { "shell.header.start": [contribution] } }), { shellSlots: ["shell.header.start", "shell.header.center", "shell.header.end"] })).toBe(true);
    expect(hasRegionContent(composition({ navigation: { primary: [{ id: "primary", label: "主要功能", zone: "primary", icon: "menu", pages: [{ id: "home", label: "首页", zone: "primary" }], children: [] }], settings: [], secondary: [] } }), { navigationGroups: true })).toBe(true);
    expect(hasRegionContent(composition(), { intrinsic: true })).toBe(true);
  });

  it("uses a 64px icon rail and a persistent 240px second-level panel on desktop", () => {
    expect(standardShellCSS).toContain(".vp-navigation-rail{width:var(--vp-shell-rail-width);flex:0 0 var(--vp-shell-rail-width)");
    expect(standardShellCSS).toContain(".vp-navigation-panel{width:var(--vp-shell-navigation-width);flex:0 0 var(--vp-shell-navigation-width)");
    expect(standardShellCSS).toContain(".vp-page-scroller{flex:1;min-height:0;overflow:auto");
    expect(standardShellCSS).toContain("@media (max-width:767px){.vp-desktop-navigation{display:none}");
  });

  it("aligns the desktop brand, second-level title and page header to one shell bar height", () => {
    expect(standardShellCSS).toContain(".vp-shell-header{height:var(--vp-shell-bar-height);flex:0 0 var(--vp-shell-bar-height)");
    expect(standardShellCSS).toContain(".vp-navigation-start{box-sizing:border-box;height:var(--vp-shell-bar-height);min-height:var(--vp-shell-bar-height)");
    expect(standardShellCSS).toContain(".vp-navigation-panel-header{box-sizing:border-box;height:var(--vp-shell-bar-height);min-height:var(--vp-shell-bar-height)");
    expect(standardShellCSS).toContain(".vp-page-header{box-sizing:border-box;height:var(--vp-shell-bar-height);min-height:var(--vp-shell-bar-height);flex:0 0 var(--vp-shell-bar-height)");
  });

  it("owns a multi-open child navigation tree with real page links", () => {
    expect(standardShellCSS).toContain(".vp-navigation-child-trigger");
    expect(standardShellCSS).toContain(".vp-navigation-link[aria-current=page]");
  });

  it("keeps semantic zone order while returning normalized groups", () => {
    const model = composition({ navigation: {
      primary: [{ id: "operations", label: "运行", zone: "primary", icon: "menu", pages: [], children: [] }],
      secondary: [{ id: "reports", label: "报表", zone: "secondary", icon: "info", pages: [], children: [] }],
      settings: [{ id: "settings", label: "设置", zone: "settings", icon: "settings", pages: [], children: [] }],
    } });
    expect(groups(model, ["primary", "secondary", "settings"]).map((group) => group.id)).toEqual(["operations", "reports", "settings"]);
  });
});
