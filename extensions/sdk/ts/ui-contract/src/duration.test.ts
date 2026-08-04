import { describe, expect, it } from "vitest";
import { durationUnits } from "./index.js";

describe("duration units", () => {
  it("keeps the portable fixed-duration unit vocabulary stable", () => {
    expect(durationUnits).toEqual(["millisecond", "second", "minute", "hour", "day", "week", "month"]);
    expect(Object.isFrozen(durationUnits)).toBe(true);
  });
});
