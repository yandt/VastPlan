import type { CollectionDensity, CollectionSpec } from "@vastplan/ui-contract";
import type { ComponentSize } from "@vastplan/ui-primitives";
import type { WorkbenchPresentationConfig } from "@vastplan/workbench-sdk";

/** Resolves a page request against the Platform Profile's permitted density set. */
export function collectionDensity(collection: CollectionSpec, presentation: WorkbenchPresentationConfig | undefined, preferred?: CollectionDensity): CollectionDensity {
  const configured = preferred ?? collection.presentation?.density ?? presentation?.collection?.defaultDensity ?? "standard";
  const allowed = presentation?.collection?.allowedDensities;
  return allowed === undefined || allowed.includes(configured) ? configured : presentation?.collection?.defaultDensity ?? "standard";
}

export function collectionDensityOptions(collection: CollectionSpec, presentation: WorkbenchPresentationConfig | undefined): readonly CollectionDensity[] {
  if (collection.preferences?.density !== true) return [];
  return presentation?.collection?.allowedDensities ?? ["compact", "standard", "comfortable"];
}

/** Collection density is the user-facing preference; component size is its internal projection. */
export function componentSizeForDensity(density: CollectionDensity): ComponentSize {
  return density === "compact" ? "sm" : density === "comfortable" ? "lg" : "md";
}
