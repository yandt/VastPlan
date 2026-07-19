import { describe, expect, it } from "vitest";
import adapter, { prioritizeRoots, topNavigationShellCSS } from "./index";
import type { PortalNavigationGroup } from "@vastplan/portal-ui";

const root = (id: string): PortalNavigationGroup => ({ id, label: id, zone: "primary", icon: "menu", pages: [], children: [] });

describe("top navigation shell layout", () => {
  it("exports an independent UI Contract 2 layout", () => {
    expect(adapter).toMatchObject({ id: "ui.shell-layout", uiContract: "2.0.0" });
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
});
