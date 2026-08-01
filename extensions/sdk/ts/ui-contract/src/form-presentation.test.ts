import { describe, expect, it } from "vitest";
import { formControlAlignments } from "./index.js";

describe("form presentation contract", () => {
  it("keeps the control alignment vocabulary closed and immutable", () => {
    expect(formControlAlignments).toEqual(["start", "end"]);
    expect(Object.isFrozen(formControlAlignments)).toBe(true);
  });
});
