import { describe, expect, it } from "vitest";
import { mergeCursorRows, normalizeNextCursor } from "./useCollectionData.js";

describe("cursor collection state", () => {
  it("appends new records while replacing duplicates with the newest server fact", () => {
    const merged = mergeCursorRows([{ id: "a", value: 1 }, { id: "b", value: 1 }], [{ id: "b", value: 2 }, { id: "c", value: 1 }], (row) => String(row.id));
    expect(merged).toEqual([{ id: "a", value: 1 }, { id: "b", value: 2 }, { id: "c", value: 1 }]);
  });

  it("stops a loader that repeats the requested cursor", () => {
    expect(() => normalizeNextCursor("next-2", "next-2")).toThrow("nextCursor");
    expect(normalizeNextCursor("next-2", "next-3")).toBe("next-3");
    expect(normalizeNextCursor(undefined, "")).toBeUndefined();
  });
});
