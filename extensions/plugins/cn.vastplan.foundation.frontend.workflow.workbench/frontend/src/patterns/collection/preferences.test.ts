import { describe, expect, it } from "vitest";
import type { CollectionSpec } from "@vastplan/ui-contract";
import { collectionPreferenceFromColumns, readCollectionColumns } from "./preferences.js";

const collection: CollectionSpec = {
  id: "services",
  title: "Services",
  view: "table",
  query: { mode: "page", defaultPageSize: 20, pageSizeOptions: [20, 50] },
  columns: [
    { key: "id", label: "ID" },
    { key: "name", label: "Name" },
    { key: "status", label: "Status", defaultVisible: false },
  ],
  preferences: { allowedColumns: ["id", "name", "status"], density: true },
};

describe("collection preferences", () => {
  it("restores governed order and visibility while retaining new columns", () => {
    expect(readCollectionColumns("tenant/portal", collection, { columns: ["name", "id"], hiddenColumns: ["id"] })).toEqual([
      { key: "name", visible: true },
      { key: "id", visible: false },
      { key: "status", visible: false },
    ]);
  });

  it("serializes stable IDs instead of UI objects", () => {
    expect(collectionPreferenceFromColumns([{ key: "name", visible: true }, { key: "id", visible: false }], { density: "compact", pageSize: 50 })).toEqual({
      columns: ["name", "id"], hiddenColumns: ["id"], density: "compact", pageSize: 50,
    });
  });
});
