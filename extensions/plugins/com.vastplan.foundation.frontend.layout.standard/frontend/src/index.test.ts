import { describe, expect, it } from "vitest";
import type { PortalSlotContribution, ShellCompositionModel } from "@vastplan/portal-ui";
import adapter from "./index";
import { hasRegionContent } from "./region-visibility";

function composition(overrides: Partial<ShellCompositionModel> = {}): ShellCompositionModel {
  return {
    pages: [],
    navigation: { primary: [], settings: [], secondary: [] },
    slots: {},
    ...overrides,
  };
}

const contribution: PortalSlotContribution = { id: "content", slot: "shell.header.start", component: () => null };

describe("standard shell layout", () => {
  it("exports only the visual layout adapter contract", () => {
    expect(adapter.id).toBe("ui.shell-layout");
    expect(adapter.uiContract).toBe("1.0.0");
    expect(adapter.Shell).toBeTypeOf("function");
    expect(adapter).not.toHaveProperty("compose");
  });

  it("collapses a region with no slots, navigation or intrinsic layout content", () => {
    expect(hasRegionContent(composition(), { slots: ["shell.header.start", "shell.header.center", "shell.header.end"] })).toBe(false);
    expect(hasRegionContent(composition({ slots: { "page.aside": [] } }), { slots: ["page.aside"] })).toBe(false);
  });

  it("keeps a region when any supported content source is present", () => {
    expect(hasRegionContent(composition({ slots: { "shell.header.start": [contribution] } }), { slots: ["shell.header.start", "shell.header.center", "shell.header.end"] })).toBe(true);
    expect(hasRegionContent(composition({ navigation: { primary: [{ id: "home", label: "首页", zone: "primary" }], settings: [], secondary: [] } }), { navigationZones: ["primary"] })).toBe(true);
    expect(hasRegionContent(composition(), { intrinsic: true })).toBe(true);
  });
});
