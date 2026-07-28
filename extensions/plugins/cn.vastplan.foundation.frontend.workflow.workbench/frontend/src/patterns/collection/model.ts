import type { ActionSpec, CollectionSelectionMode } from "@vastplan/ui-contract";

export type CollectionRow = Record<string, unknown>;

export interface CollectionColumnPreference {
  key: string;
  visible: boolean;
}

/** Row selection exists only as the input surface for authorized bulk actions. */
export function collectionSelectionMode(actions: readonly ActionSpec[], requested: CollectionSelectionMode | undefined): CollectionSelectionMode {
  if (!actions.some((action) => action.placement === "collection.bulk")) return "none";
  return requested === "single" ? "single" : "multiple";
}
