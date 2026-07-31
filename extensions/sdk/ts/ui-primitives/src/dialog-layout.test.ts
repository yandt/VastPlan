import { describe, expect, it } from "vitest";
import { dialogBodyStyle, dialogFrameStyle, dialogViewportMaximum } from "./dialog-layout.js";

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
});
