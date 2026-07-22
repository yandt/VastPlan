import { describe, expect, it } from "vitest";
import type { CollectionSpec } from "@vastplan/ui-contract";
import { shouldAutoApplyCollectionFilters } from "./CollectionFilters.js";
import { collectionDensity, collectionDensityOptions } from "./density.js";

const collection: CollectionSpec = {
  id: "units",
  title: "Units",
  view: "table",
  query: { mode: "page", defaultPageSize: 20, pageSizeOptions: [20] },
  columns: [{ key: "id", label: "ID" }],
};

describe("collectionDensity", () => {
  it("uses the page preference when it is allowed by the Platform Profile", () => {
    expect(collectionDensity({ ...collection, presentation: { density: "compact" } }, { collection: { defaultDensity: "standard", allowedDensities: ["compact", "standard"] } })).toBe("compact");
  });

  it("falls back to the governed default when a page asks for a disallowed density", () => {
    expect(collectionDensity({ ...collection, presentation: { density: "comfortable" } }, { collection: { defaultDensity: "compact", allowedDensities: ["compact", "standard"] } })).toBe("compact");
  });

  it("uses a valid user preference and only exposes governed choices when opted in", () => {
    expect(collectionDensity({ ...collection, preferences: { density: true } }, { collection: { defaultDensity: "standard", allowedDensities: ["compact", "standard"] } }, "compact")).toBe("compact");
    expect(collectionDensityOptions({ ...collection, preferences: { density: true } }, { collection: { allowedDensities: ["compact", "standard"] } })).toEqual(["compact", "standard"]);
    expect(collectionDensityOptions(collection, undefined)).toEqual([]);
  });
});

describe("collection filter interaction", () => {
  it("uses direct-query interaction while the desktop filter grid has fewer than two rows", () => {
    expect(shouldAutoApplyCollectionFilters([{ id: "name", label: "Name", kind: "text" }, { id: "status", label: "Status", kind: "select" }])).toBe(true);
  });

  it("requires an explicit query after the filter grid reaches two rows", () => {
    expect(shouldAutoApplyCollectionFilters([{ id: "a", label: "A", kind: "text" }, { id: "b", label: "B", kind: "text" }, { id: "c", label: "C", kind: "text" }, { id: "d", label: "D", kind: "text" }])).toBe(false);
  });
});
