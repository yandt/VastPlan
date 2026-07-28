import { describe, expect, it } from "vitest";
import { compactActionVisualRecipe } from "./visual-recipes.js";

describe("visual recipes", () => {
  it("keeps compact record actions governed by one immutable recipe", () => {
    expect(compactActionVisualRecipe).toEqual({
      control: { edge: 18, iconEdge: 12, radius: 2 },
      menu: { itemHeight: 28, itemInlinePadding: 8, minWidth: 180, surfacePadding: 3, radius: 2, borderInlineEnd: 0 },
    });
    expect(Object.isFrozen(compactActionVisualRecipe)).toBe(true);
    expect(Object.isFrozen(compactActionVisualRecipe.control)).toBe(true);
    expect(Object.isFrozen(compactActionVisualRecipe.menu)).toBe(true);
  });
});
