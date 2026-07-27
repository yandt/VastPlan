import { describe, expect, it } from "vitest";
import type { CollectionSpec } from "@vastplan/ui-contract";
import { collectionFilterActionSpan, collectionFilterColumns, shouldAutoApplyCollectionFilters } from "./CollectionFilters.js";
import { collectionFilterSchema } from "./filter-schema.js";
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
  it("renders each filter label as an accessible in-control placeholder", () => {
    const schema = collectionFilterSchema([{ id: "status", label: "状态", kind: "select" }]);
    expect(schema.uiSchema).toEqual({ status: { "ui:placeholder": "", "ui:options": { label: false } } });
    expect(schema.uiLocalization).toEqual({ "/status/ui:placeholder": "状态" });
  });

  it("uses direct-query interaction while the desktop filter grid has fewer than two rows", () => {
    expect(shouldAutoApplyCollectionFilters([{ id: "name", label: "Name", kind: "text" }, { id: "status", label: "Status", kind: "select" }])).toBe(true);
  });

  it("requires an explicit query after the default four-column grid reaches two rows", () => {
    expect(shouldAutoApplyCollectionFilters([{ id: "a", label: "A", kind: "text" }, { id: "b", label: "B", kind: "text" }, { id: "c", label: "C", kind: "text" }, { id: "d", label: "D", kind: "text" }])).toBe(true);
    expect(shouldAutoApplyCollectionFilters([{ id: "a", label: "A", kind: "text" }, { id: "b", label: "B", kind: "text" }, { id: "c", label: "C", kind: "text" }, { id: "d", label: "D", kind: "text" }, { id: "e", label: "E", kind: "text" }])).toBe(false);
  });

  it("honors explicit filter column counts when choosing direct-query mode", () => {
    const filters = [{ id: "a", label: "A", kind: "text" }, { id: "b", label: "B", kind: "text" }, { id: "c", label: "C", kind: "text" }] as const;
    expect(shouldAutoApplyCollectionFilters(filters, 2)).toBe(false);
    expect(shouldAutoApplyCollectionFilters(filters, { xs: 1, md: 2, xl: 3 })).toBe(true);
    expect(collectionFilterColumns({ columns: 2 })).toBe(2);
  });

  it("spans the remaining row so multi-line filter actions end in the final column at every breakpoint", () => {
    const filters = [{ id: "a", label: "A", kind: "text" }, { id: "b", label: "B", kind: "text" }, { id: "c", label: "C", kind: "text" }, { id: "d", label: "D", kind: "text" }, { id: "e", label: "E", kind: "text" }] as const;
    expect(collectionFilterActionSpan(filters, 4)).toBe(3);
    expect(collectionFilterActionSpan(filters, { xs: 1, md: 2, xl: 4 })).toEqual({ xs: 1, sm: 1, md: 1, lg: 1, xl: 3 });
  });
});
