import { describe, expect, it } from "vitest";
import { dialogBodyStyle, dialogFrameStyle, dialogViewportMaximum, formDialogViewportMaximum } from "./dialog-layout.js";

describe("dialog layout", () => {
  it("preserves content-driven sizing while limiting every frame to 90vh", () => {
    expect(dialogViewportMaximum).toBe("90vh");
    expect(dialogFrameStyle()).toEqual({ maxHeight: "90vh" });
    expect(dialogFrameStyle(1200)).toEqual({ height: "min(1200px, 90vh)", maxHeight: "90vh" });
  });

  it("assigns overflow to the body below dialog chrome", () => {
    expect(dialogBodyStyle("visible")).toEqual({ minHeight: 0 });
    expect(dialogBodyStyle("scroll")).toEqual({ minHeight: 0, maxHeight: "calc(90vh - 144px)", overflowY: "auto" });
  });

  it("allows FormDialog to use the governed 95vh viewport maximum", () => {
    expect(formDialogViewportMaximum).toBe("95vh");
    expect(dialogFrameStyle(undefined, formDialogViewportMaximum)).toEqual({ maxHeight: "95vh" });
    expect(dialogFrameStyle(1200, formDialogViewportMaximum)).toEqual({ height: "min(1200px, 95vh)", maxHeight: "95vh" });
    expect(dialogBodyStyle("scroll", formDialogViewportMaximum)).toEqual({ minHeight: 0, maxHeight: "calc(95vh - 144px)", overflowY: "auto" });
  });
});
