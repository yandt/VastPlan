import { describe, expect, it } from "vitest";
import type { CollectionSpec } from "@vastplan/ui-contract";
import { resolveTableVirtualization } from "./table-virtualization.js";

function collection(virtualization?: "auto" | "always" | "off"): CollectionSpec {
  return {
    id: "items",
    title: "Items",
    view: "table",
    query: { mode: "page", defaultPageSize: 20, pageSizeOptions: [20, 100] },
    columns: [],
    table: virtualization === undefined ? undefined : { virtualization },
  };
}

describe("resolveTableVirtualization", () => {
  it("keeps small tables unvirtualized and enables large tables automatically", () => {
    expect(resolveTableVirtualization(collection(), 79, "standard").enabled).toBe(false);
    expect(resolveTableVirtualization(collection(), 80, "standard").enabled).toBe(true);
  });

  it("honors explicit policy without exposing framework props", () => {
    expect(resolveTableVirtualization(collection("always"), 1, "compact")).toEqual({ enabled: true, rowHeight: 40, viewportHeight: 480, overscan: 4 });
    expect(resolveTableVirtualization(collection("off"), 1000, "comfortable").enabled).toBe(false);
  });
});
