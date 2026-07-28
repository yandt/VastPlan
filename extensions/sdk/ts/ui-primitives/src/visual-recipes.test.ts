import { describe, expect, it } from "vitest";
import { componentSizeRecipes, componentVariantRecipes } from "./visual-recipes.js";

describe("visual recipes", () => {
  it("keeps all three component sizes governed by immutable recipes", () => {
    expect(componentSizeRecipes.iconButton.sm).toEqual({ edge: 18, iconEdge: 12, radius: 2 });
    expect(componentSizeRecipes.iconButton.md).toEqual({ edge: 32, iconEdge: 16, radius: 4 });
    expect(componentSizeRecipes.iconButton.lg).toEqual({ edge: 44, iconEdge: 20, radius: 6 });
    expect(componentSizeRecipes.menu.sm).toMatchObject({ itemHeight: 28, minWidth: 180, surfacePadding: 3 });
    expect(componentSizeRecipes.menu.md).toMatchObject({ itemHeight: 36, minWidth: 200, surfacePadding: 4 });
    expect(componentSizeRecipes.menu.lg).toMatchObject({ itemHeight: 44, minWidth: 220, surfacePadding: 6 });
    expect(Object.isFrozen(componentSizeRecipes)).toBe(true);
    expect(Object.isFrozen(componentSizeRecipes.control.sm)).toBe(true);
    expect(Object.isFrozen(componentSizeRecipes.menu.lg)).toBe(true);
    expect(componentVariantRecipes.menu.action).toEqual({ borderInlineEnd: 0, width: "max-content", minWidth: 112, maxWidth: 280, overflow: "hidden" });
    expect(componentVariantRecipes.menu.actionItem).toEqual({ display: "flex", alignItems: "center", gap: "8px" });
  });
});
