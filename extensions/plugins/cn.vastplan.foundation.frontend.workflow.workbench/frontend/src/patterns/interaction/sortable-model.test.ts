import { describe, expect, it } from "vitest";
import { moveSortableItem } from "./sortable-model.js";

describe("sortable model", () => {
  it("moves items immutably and rejects out-of-range destinations", () => {
    const items = ["id", "name", "status"] as const;
    expect(moveSortableItem(items, 0, 2)).toEqual(["name", "status", "id"]);
    expect(moveSortableItem(items, 2, 0)).toEqual(["status", "id", "name"]);
    expect(moveSortableItem(items, 0, 3)).toEqual(items);
  });
});
