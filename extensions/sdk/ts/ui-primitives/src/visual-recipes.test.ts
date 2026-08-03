import { describe, expect, it } from "vitest";
import { componentSizeRecipes, componentVariantRecipes, pageBodyLayoutRecipes, resolvePageBodyMaxWidth } from "./visual-recipes.js";

describe("visual recipes", () => {
  it("keeps all four component sizes governed by immutable recipes", () => {
    expect(componentSizeRecipes.iconButton.xs).toEqual({ edge: 18, iconEdge: 12, radius: 2 });
    expect(componentSizeRecipes.iconButton.sm).toEqual({ edge: 24, iconEdge: 12, radius: 3 });
    expect(componentSizeRecipes.iconButton.md).toEqual({ edge: 32, iconEdge: 16, radius: 4 });
    expect(componentSizeRecipes.iconButton.lg).toEqual({ edge: 44, iconEdge: 20, radius: 6 });
    expect(componentSizeRecipes.menu.xs).toMatchObject({ itemHeight: 28, minWidth: 180, surfacePadding: 3 });
    expect(componentSizeRecipes.menu.sm).toMatchObject({ itemHeight: 32, minWidth: 190, surfacePadding: 3 });
    expect(componentSizeRecipes.menu.md).toMatchObject({ itemHeight: 36, minWidth: 200, surfacePadding: 4 });
    expect(componentSizeRecipes.menu.lg).toMatchObject({ itemHeight: 44, minWidth: 220, surfacePadding: 6 });
    expect(componentSizeRecipes.layout).toMatchObject({
      xs: { gap: 4, flowGap: 8, padding: 8, sectionGap: 8, outerMargin: 0 },
      sm: { gap: 8, flowGap: 12, padding: 12, sectionGap: 12, outerMargin: 0 },
      md: { gap: 16, flowGap: 16, padding: 16, sectionGap: 24, outerMargin: 0 },
      lg: { gap: 24, flowGap: 24, padding: 24, sectionGap: 32, outerMargin: 0 },
    });
    expect(Object.isFrozen(componentSizeRecipes)).toBe(true);
    expect(Object.isFrozen(componentSizeRecipes.control.xs)).toBe(true);
    expect(Object.isFrozen(componentSizeRecipes.menu.lg)).toBe(true);
    expect(componentVariantRecipes.menu.action).toEqual({ borderInlineEnd: 0, width: "max-content", minWidth: 112, maxWidth: 280, overflow: "hidden", padding: "4px" });
    expect(componentVariantRecipes.menu.actionItem).toEqual({ display: "flex", alignItems: "center", width: "100%", gap: "6px", paddingInline: "12px 6px" });
    expect(componentVariantRecipes.menu.shellNavigation).toEqual({ itemHeight: 44, itemInlinePadding: 12, minWidth: 220, surfacePadding: 4, radius: 8, childIndent: 12 });
  });

  it("governs page body widths and lets the narrower page or Shell limit win", () => {
    expect(pageBodyLayoutRecipes).toEqual({ fluid: {}, large: { maxWidth: 1280 }, medium: { maxWidth: 960 }, small: { maxWidth: 720 } });
    expect(resolvePageBodyMaxWidth(undefined, false)).toBe(1280);
    expect(resolvePageBodyMaxWidth("fluid", false)).toBeUndefined();
    expect(resolvePageBodyMaxWidth("fluid", true)).toBe(1280);
    expect(resolvePageBodyMaxWidth("small", false)).toBe(720);
    expect(resolvePageBodyMaxWidth("medium", true)).toBe(960);
    expect(Object.isFrozen(pageBodyLayoutRecipes.small)).toBe(true);
  });
});
