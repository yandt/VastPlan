import { describe, expect, it } from "vitest";
import { defineCollectionPage } from "./index.js";

describe("defineCollectionPage", () => {
  it("keeps the serializable collection contract and runtime loader together without exposing a component", async () => {
    const page = defineCollectionPage({
      id: "revisions", path: "/revisions", title: "Revisions",
      collection: { id: "revisions", title: "Revisions", view: "table", query: { mode: "page", defaultPageSize: 20, pageSizeOptions: [20] }, columns: [{ key: "id", label: "ID" }] },
      async load() { return { items: [], total: 0 }; },
    });
    expect(Object.isFrozen(page)).toBe(true);
    await expect(page.load({ mode: "page", page: 1, pageSize: 20, filters: {} }, new AbortController().signal)).resolves.toEqual({ items: [], total: 0 });
  });

  it("requires card collections to use the shared cursor contract", () => {
    expect(() => defineCollectionPage({
      id: "cards", path: "/cards", title: "Cards",
      collection: { id: "cards", title: "Cards", view: "cards", query: { mode: "page", defaultPageSize: 20, pageSizeOptions: [20] }, columns: [] },
      async load() { return { items: [] }; },
    })).toThrow("cursor");
    expect(() => defineCollectionPage({
      id: "cards", path: "/cards", title: "Cards",
      collection: { id: "cards", title: "Cards", view: "cards", query: { mode: "cursor", defaultPageSize: 20, pageSizeOptions: [20] }, columns: [] },
      async load() { return { items: [] }; },
    })).toThrow("card");
  });
});
