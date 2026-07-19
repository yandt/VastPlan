import { describe, expect, it } from "vitest";
import type { PortalSlotContribution, ShellCompositionModel } from "@vastplan/portal-ui";
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

const contribution: PortalSlotContribution<"shell.header.start"> & { pluginID: string } = { id: "content", pluginID: "com.vastplan.foundation.test", slot: "shell.header.start", component: () => null };

describe("standard shell layout", () => {
  it("exports only the visual layout adapter contract", () => {
    expect(adapter.id).toBe("ui.shell-layout");
    expect(adapter.uiContract).toBe("1.0.0");
    expect(adapter.Shell).toBeTypeOf("function");
    expect(adapter).not.toHaveProperty("compose");
  });

  it("collapses a region with no slots, navigation or intrinsic layout content", () => {
    expect(hasRegionContent(composition(), { shellSlots: ["shell.header.start", "shell.header.center", "shell.header.end"] })).toBe(false);
    expect(hasRegionContent(composition({ pageSlots: { "page.aside": [] } }), { pageSlots: ["page.aside"] })).toBe(false);
  });

  it("keeps a region when any supported content source is present", () => {
    expect(hasRegionContent(composition({ shellSlots: { "shell.header.start": [contribution] } }), { shellSlots: ["shell.header.start", "shell.header.center", "shell.header.end"] })).toBe(true);
    expect(hasRegionContent(composition({ navigation: { primary: [{ id: "primary", label: "主要功能", zone: "primary", icon: "menu", pages: [{ id: "home", label: "首页", zone: "primary" }] }], settings: [], secondary: [] } }), { navigationGroups: true })).toBe(true);
    expect(hasRegionContent(composition(), { intrinsic: true })).toBe(true);
  });

  it("uses a 64px icon rail and a persistent 240px second-level panel on desktop", () => {
    expect(standardShellCSS).toContain(".vp-navigation-rail{width:64px;flex:0 0 64px");
    expect(standardShellCSS).toContain(".vp-navigation-panel{width:240px;flex:0 0 240px");
    expect(standardShellCSS).toContain(".vp-page-scroller{flex:1;min-height:0;overflow:auto");
    expect(standardShellCSS).toContain("@media (max-width:767px){.vp-desktop-navigation{display:none}");
  });

  it("keeps semantic zone order while returning normalized groups", () => {
    const model = composition({ navigation: {
      primary: [{ id: "operations", label: "运行", zone: "primary", icon: "menu", pages: [] }],
      secondary: [{ id: "reports", label: "报表", zone: "secondary", icon: "info", pages: [] }],
      settings: [{ id: "settings", label: "设置", zone: "settings", icon: "settings", pages: [] }],
    } });
    expect(groups(model, ["primary", "secondary", "settings"]).map((group) => group.id)).toEqual(["operations", "reports", "settings"]);
  });
});
