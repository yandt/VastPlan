import { describe, expect, it } from "vitest";
import { toggleColumnVisibility } from "./column-preference-actions.js";

const columns = [
  { key: "id", visible: true },
  { key: "name", visible: false },
  { key: "status", visible: true },
] as const;

describe("column preference actions", () => {
  it("toggles only the requested column visibility", () => {
    expect(toggleColumnVisibility(columns, "name")).toEqual([
      { key: "id", visible: true },
      { key: "name", visible: true },
      { key: "status", visible: true },
    ]);
  });
});
