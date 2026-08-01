import { describe, expect, it } from "vitest";
import { accessMethodPresentation } from "./access-method-selector";

describe("Access method selector", () => {
  it("uses tabs for two or three methods and a select for larger sets", () => {
    expect(accessMethodPresentation(2)).toBe("tabs");
    expect(accessMethodPresentation(3)).toBe("tabs");
    expect(accessMethodPresentation(4)).toBe("select");
  });
});
