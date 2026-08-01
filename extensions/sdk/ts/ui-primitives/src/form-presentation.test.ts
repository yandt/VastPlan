import { describe, expect, it } from "vitest";
import { formControlSize, formGridTemplate, resolveFormPresentation } from "./form-presentation.js";

describe("form presentation", () => {
  it("defaults to the standard inline form recipe", () => {
    expect(resolveFormPresentation(undefined)).toMatchObject({ preset: "standard", layout: "horizontal", labelPlacement: "inline", controlAlignment: "end" });
  });

  it("maps governed presets without overriding explicit choices", () => {
    expect(resolveFormPresentation({ preset: "guided" })).toMatchObject({ layout: "vertical", labelPlacement: "stacked", navigation: "steps" });
    expect(resolveFormPresentation({ preset: "guided", labelPlacement: "inline", navigation: "tabs" })).toMatchObject({ labelPlacement: "inline", navigation: "tabs" });
    expect(resolveFormPresentation({ controlAlignment: "start" })).toMatchObject({ controlAlignment: "start" });
    expect(formControlSize({ preset: "compact" })).toBe("xs");
  });

  it("converts validated percentage weights to gap-safe grid fractions", () => {
    expect(formGridTemplate({ columns: 2, columnWidths: [35, 65] })).toBe("minmax(0, 35fr) minmax(0, 65fr)");
    expect(formGridTemplate(undefined, { id: "main", fields: ["/a", "/b", "/c"], columns: 3 })).toBe("repeat(3, minmax(0, 1fr))");
  });
});
