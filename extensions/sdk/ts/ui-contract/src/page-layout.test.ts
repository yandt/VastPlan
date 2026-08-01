import { describe, expect, it } from "vitest";
import { pageBodyLayouts } from "./index.js";

describe("page body layout contract", () => {
  it("exposes one closed immutable semantic width vocabulary", () => {
    expect(pageBodyLayouts).toEqual(["fluid", "large", "medium", "small"]);
    expect(Object.isFrozen(pageBodyLayouts)).toBe(true);
  });
});
