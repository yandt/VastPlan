import { describe, expect, it } from "vitest";
import { reorderColumns, toggleColumnVisibility } from "./column-preference-actions.js";

const columns = [
  { key: "id", visible: true },
  { key: "name", visible: false },
  { key: "status", visible: true },
] as const;

describe("column preference actions", () => {
  it("moves a dragged column to the dropped column position", () => {
    expect(reorderColumns(columns, "id", "status").map((column) => column.key)).toEqual(["name", "status", "id"]);
    expect(reorderColumns(columns, "status", "id").map((column) => column.key)).toEqual(["status", "id", "name"]);
  });

  it("ignores unknown or identical drag targets", () => {
    expect(reorderColumns(columns, "missing", "status")).toEqual(columns);
    expect(reorderColumns(columns, "id", "id")).toEqual(columns);
  });

  it("toggles only the requested column visibility", () => {
    expect(toggleColumnVisibility(columns, "name")).toEqual([
      { key: "id", visible: true },
      { key: "name", visible: true },
      { key: "status", visible: true },
    ]);
  });
});
