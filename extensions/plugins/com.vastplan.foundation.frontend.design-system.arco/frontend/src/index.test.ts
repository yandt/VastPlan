import { describe, expect, it } from "vitest";
import { arcoDesignSystem, arcoPortalUIComponents } from "./index";

describe("Arco portal UI adapter", () => {
  it("implements the complete stable component surface", () => {
    expect(Object.keys(arcoPortalUIComponents).sort()).toEqual([
      "Breadcrumb", "Busy", "Button", "CommandPalette", "Descriptions", "Dialog", "Divider", "Drawer",
      "EmptyState", "ErrorState", "FilterBar", "FormRenderer", "Grid", "GridItem", "Icon", "Menu", "Page", "Pagination",
      "Panel", "PortalShell", "Skeleton", "Stack", "Status", "Table", "Tabs", "theme",
    ].sort());
  });

  it("declares every capability implemented by the adapter", () => {
    expect(arcoDesignSystem.capabilities).toEqual(expect.arrayContaining([
      "layout", "menu", "navigation", "overlay", "form", "data", "feedback", "theme",
    ]));
  });
});
