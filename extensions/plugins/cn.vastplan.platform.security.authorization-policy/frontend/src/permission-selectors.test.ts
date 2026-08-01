import { describe, expect, it } from "vitest";
import { normalizePermissionSelectorInputs } from "./permission-selectors";

describe("permission selector inputs", () => {
  it("trims, deduplicates and classifies exact and glob selectors", () => {
    expect(normalizePermissionSelectorInputs([" platform.portal.read ", "platform.**", "platform.portal.read", ""])).toEqual([
      { kind: "exact", value: "platform.portal.read" },
      { kind: "glob", value: "platform.**" },
    ]);
  });

  it("ignores non-string form values", () => {
    expect(normalizePermissionSelectorInputs([null, 1, false])).toEqual([]);
  });
});
