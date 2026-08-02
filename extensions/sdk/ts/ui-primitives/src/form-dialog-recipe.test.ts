import { describe, expect, it } from "vitest";
import { componentSizeRecipes } from "./visual-recipes.js";

describe("FormDialog visual recipe", () => {
  it("keeps form overlays denser than general layouts without reducing their control size", () => {
    expect(componentSizeRecipes.formDialog).toEqual({
      xs: { bodyPadding: 8, contentGap: 4, inlineLabelWidth: 80 },
      sm: { bodyPadding: 10, contentGap: 8, inlineLabelWidth: 88 },
      md: { bodyPadding: 12, contentGap: 8, inlineLabelWidth: 96 },
      lg: { bodyPadding: 16, contentGap: 16, inlineLabelWidth: 112 },
    });
    expect(componentSizeRecipes.formDialog.md.inlineLabelWidth).toBeLessThan(112);
  });
});
