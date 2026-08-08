import { describe, expect, it } from "vitest";
import { uiContractVersion } from "@vastplan/ui-contract";
import type { PortalNavigationCollection, PortalNavigationGroup, PortalResolvedPageNavigation, PortalSlotContribution, ShellCompositionModel } from "@vastplan/ui-primitives";
import adapter, { collections, firstNavigablePageID, navigationRailCollections, standardShellCSS } from "./index";
import { hasRegionContent } from "./region-visibility";

function composition(overrides: Partial<ShellCompositionModel> = {}): ShellCompositionModel {
  return {
    pages: [],
    navigation: { primary: [], settings: [], secondary: [] },
    navigationCollections: { primary: [], settings: [], secondary: [] },
    shellSlots: {},
    pageSlots: {},
    ...overrides,
  };
}

const contribution: PortalSlotContribution<"shell.header.start"> & { pluginID: string } = { id: "content", pluginID: "cn.vastplan.foundation.test", slot: "shell.header.start", component: () => null };
const icon = { kind: "semantic" as const, name: "menu" as const };
const navigationPage = (id: string, zone: "primary" | "secondary" | "settings" = "primary"): PortalResolvedPageNavigation => ({ id, label: id, zone, groupID: "test/main", parentMenuRef: { pluginID: "test", nodeID: "main" } });
const navigationGroup = (id: string, zone: "primary" | "secondary" | "settings" = "primary"): PortalNavigationGroup => ({ id, label: id, zone, icon, pages: [], children: [] });
const navigationCollection = (group: PortalNavigationGroup): PortalNavigationCollection => ({ kind: "group", id: `group:${group.id}`, label: group.label, zone: group.zone, icon: group.icon, groups: [group] });

describe("standard shell layout", () => {
  it("exports only the visual layout adapter contract", () => {
    expect(adapter.id).toBe("standard");
    expect(adapter.shell).toBe("ui.structure.shell");
    expect(adapter.uiContract).toBe(uiContractVersion);
    expect(adapter.Shell).toBeTypeOf("function");
    expect(adapter).not.toHaveProperty("compose");
  });

  it("collapses a region with no slots, navigation or intrinsic layout content", () => {
    expect(hasRegionContent(composition(), { shellSlots: ["shell.header.start", "shell.header.center", "shell.header.end"] })).toBe(false);
    expect(hasRegionContent(composition({ pageSlots: { "page.aside": [] } }), { pageSlots: ["page.aside"] })).toBe(false);
  });

  it("keeps a region when any supported content source is present", () => {
    expect(hasRegionContent(composition({ shellSlots: { "shell.header.start": [contribution] } }), { shellSlots: ["shell.header.start", "shell.header.center", "shell.header.end"] })).toBe(true);
    expect(hasRegionContent(composition({ navigation: { primary: [{ ...navigationGroup("primary"), pages: [navigationPage("home")] }], settings: [], secondary: [] } }), { navigation: true })).toBe(true);
    expect(hasRegionContent(composition(), { intrinsic: true })).toBe(true);
  });

  it("uses a 64px icon rail and a persistent 240px second-level panel on desktop", () => {
    expect(standardShellCSS).toContain(".vp-navigation-rail{width:var(--vp-shell-rail-width);flex:0 0 var(--vp-shell-rail-width)");
    expect(standardShellCSS).toContain(".vp-navigation-panel{width:var(--vp-shell-navigation-width);flex:0 0 var(--vp-shell-navigation-width)");
    expect(standardShellCSS).toContain(".vp-page-scroller{flex:1;min-height:0;overflow:auto");
    expect(standardShellCSS).toContain("@media (max-width:767px){.vp-desktop-navigation{display:none}");
  });

  it("uses the renderer surface token for the page body", () => {
    expect(standardShellCSS).toContain(".vp-page-scroller{flex:1;min-height:0;overflow:auto;overscroll-behavior:contain;background:var(--vp-shell-surface)}");
    expect(standardShellCSS).toContain("padding:var(--vp-page-content-start) 24px 24px");
  });

  it("aligns the desktop brand, second-level title and page header to one shell bar height", () => {
    expect(standardShellCSS).toContain(".vp-shell-header{height:var(--vp-shell-bar-height);flex:0 0 var(--vp-shell-bar-height)");
    expect(standardShellCSS).toContain(".vp-navigation-start{box-sizing:border-box;height:var(--vp-shell-bar-height);min-height:var(--vp-shell-bar-height)");
    expect(standardShellCSS).toContain(".vp-navigation-panel-header{box-sizing:border-box;height:var(--vp-shell-bar-height);min-height:var(--vp-shell-bar-height)");
    expect(standardShellCSS).toContain(".vp-page-header{box-sizing:border-box;height:var(--vp-shell-bar-height);min-height:var(--vp-shell-bar-height);flex:0 0 var(--vp-shell-bar-height)");
  });

  it("delegates the persistent child navigation tree to the shared renderer Menu", () => {
    expect(standardShellCSS).not.toContain(".vp-navigation-child-trigger");
    expect(standardShellCSS).not.toContain(".vp-navigation-link[aria-current=page]");
  });

  it("does not retain a layout-owned navigation item skin", () => {
    expect(standardShellCSS).not.toContain(".vp-navigation-tree");
    expect(standardShellCSS).not.toContain(".vp-navigation-action");
    expect(standardShellCSS).not.toContain(".vp-navigation-child[data-active]");
  });

  it("keeps semantic zone order while returning presentation collections", () => {
    const operations = navigationCollection(navigationGroup("operations"));
    const reports = navigationCollection(navigationGroup("reports", "secondary"));
    const settings = navigationCollection(navigationGroup("settings", "settings"));
    const model = composition({ navigationCollections: { primary: [operations], secondary: [reports], settings: [settings] } });
    expect(collections(model, ["primary", "secondary", "settings"]).map((collection) => collection.id)).toEqual(["group:operations", "group:reports", "group:settings"]);
  });

  it("places system settings in the main rail while keeping the account control fixed at the bottom", () => {
    const operations = navigationCollection(navigationGroup("operations"));
    const reports = navigationCollection(navigationGroup("reports", "secondary"));
    const account = navigationCollection(navigationGroup("vastplan.host/account", "secondary"));
    const settings = navigationCollection(navigationGroup("settings", "settings"));
    const model = composition({ navigationCollections: { primary: [operations], secondary: [reports, account], settings: [settings] } });
    expect(navigationRailCollections(model).map((collection) => collection.id)).toEqual(["group:operations", "group:reports", "group:settings"]);
  });

  it("enters the first direct page, then the first nested page, after a main-menu switch", () => {
    expect(firstNavigablePageID(navigationCollection({ ...navigationGroup("direct"), pages: [navigationPage("first")], children: [{ ...navigationGroup("nested"), parentID: "direct", pages: [navigationPage("later")] }] }))).toBe("first");
    expect(firstNavigablePageID(navigationCollection({ ...navigationGroup("nested"), children: [{ ...navigationGroup("child"), parentID: "nested", pages: [navigationPage("first-child")] }] }))).toBe("first-child");
    expect(firstNavigablePageID(navigationCollection(navigationGroup("empty")))).toBeUndefined();
  });
});
