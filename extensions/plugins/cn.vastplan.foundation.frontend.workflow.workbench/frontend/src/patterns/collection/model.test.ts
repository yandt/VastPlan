import { describe, expect, it } from "vitest";
import type { ActionSpec } from "@vastplan/ui-contract";
import { collectionSelectionMode } from "./model.js";

const action = (placement: ActionSpec["placement"]): ActionSpec => ({ id: placement, label: placement, icon: "info", placement });

describe("collectionSelectionMode", () => {
  it("hides selection when the collection has no bulk action", () => {
    expect(collectionSelectionMode([], "multiple")).toBe("none");
    expect(collectionSelectionMode([action("record.row")], "single")).toBe("none");
  });

  it("enables bulk selection and defaults it to multiple", () => {
    expect(collectionSelectionMode([action("collection.bulk")], undefined)).toBe("multiple");
    expect(collectionSelectionMode([action("collection.bulk")], "single")).toBe("single");
  });
});
