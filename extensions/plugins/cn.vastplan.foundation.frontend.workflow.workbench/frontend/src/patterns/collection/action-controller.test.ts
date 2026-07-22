import { describe, expect, it, vi } from "vitest";
import type { ActionSpec } from "@vastplan/ui-contract";
import type { CollectionPageDefinition } from "@vastplan/workbench-sdk";
import { collectionPageActionController } from "./action-controller.js";

const page = {
  id: "test", path: "/test", title: "Test",
  collection: { id: "test", title: "Test", view: "table", query: { mode: "page", defaultPageSize: 20, pageSizeOptions: [20] }, columns: [] },
  async load() { return { items: [], total: 0 }; },
} satisfies CollectionPageDefinition;

describe("CollectionPageActionController", () => {
  it("bridges the page header to the mounted collection workflow", () => {
    const controller = collectionPageActionController(page);
    const action: ActionSpec = { id: "create", label: "Create", icon: "add", placement: "page.primary" };
    const handler = vi.fn();
    const listener = vi.fn();
    const unsubscribe = controller.subscribe(listener);
    const unbind = controller.bind({ selectedCount: 2, visibleActionIDs: new Set([action.id]) }, handler);

    expect(controller.getSnapshot()).toMatchObject({ ready: true, selectedCount: 2 });
    controller.run(action);
    expect(handler).toHaveBeenCalledWith(action);
    expect(listener).toHaveBeenCalled();

    unbind();
    expect(controller.getSnapshot().ready).toBe(false);
    unsubscribe();
  });
});
