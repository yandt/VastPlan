import type { CollectionDensity, CollectionSpec } from "@vastplan/ui-contract";
import type { TableProps } from "@vastplan/ui-primitives";

const automaticThreshold = 80;
const viewportRows = 12;
const overscanRows = 4;

const rowHeights: Record<CollectionDensity, number> = {
  compact: 40,
  standard: 48,
  comfortable: 56,
};

/** Keeps visual tuning in Workbench while the page definition only declares intent. */
export function resolveTableVirtualization(
  collection: CollectionSpec,
  rowCount: number,
  density: CollectionDensity,
): NonNullable<TableProps["virtualization"]> {
  const mode = collection.table?.virtualization ?? "auto";
  const enabled = mode === "always" || (mode === "auto" && rowCount >= automaticThreshold);
  const rowHeight = rowHeights[density];
  return {
    enabled,
    rowHeight,
    viewportHeight: rowHeight * viewportRows,
    overscan: overscanRows,
  };
}
