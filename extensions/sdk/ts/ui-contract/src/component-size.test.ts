import { describe, expect, it } from "vitest";
import { componentSizes, overlayWidths } from "./component-size.js";

describe("component size contract", () => {
  it("keeps one ordered four-level vocabulary", () => {
    expect(componentSizes).toEqual(["xs", "sm", "md", "lg"]);
    expect(overlayWidths).toEqual(["sm", "md", "lg"]);
  });
});
