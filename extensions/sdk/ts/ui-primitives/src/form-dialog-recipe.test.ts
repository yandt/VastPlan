import { describe, expect, it } from "vitest";
import { componentSizeRecipes } from "./visual-recipes.js";

describe("FormDialog visual recipe", () => {
  it("keeps form overlays denser than general layouts without reducing their control size", () => {
    expect(componentSizeRecipes.formDialog).toEqual({
      xs: { bodyPadding: 8, contentGap: 4, inlineLabelMinWidth: 80 },
      sm: { bodyPadding: 10, contentGap: 8, inlineLabelMinWidth: 88 },
      md: { bodyPadding: 12, contentGap: 8, inlineLabelMinWidth: 96 },
      lg: { bodyPadding: 16, contentGap: 16, inlineLabelMinWidth: 112 },
    });
    expect(componentSizeRecipes.formDialog.md.inlineLabelMinWidth).toBeLessThan(112);
  });
});
