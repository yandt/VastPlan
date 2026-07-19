import type { CollectionDensity, CollectionSpec } from "@vastplan/ui-contract";
import type { WorkbenchPresentationConfig } from "@vastplan/workbench-sdk";

/** Resolves a page request against the Platform Profile's permitted density set. */
export function collectionDensity(collection: CollectionSpec, presentation: WorkbenchPresentationConfig | undefined): CollectionDensity {
  const configured = collection.presentation?.density ?? presentation?.collection?.defaultDensity ?? "standard";
  const allowed = presentation?.collection?.allowedDensities;
  return allowed === undefined || allowed.includes(configured) ? configured : presentation?.collection?.defaultDensity ?? "standard";
}
