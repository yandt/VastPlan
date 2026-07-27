import type { CollectionColumnPreference } from "./model.js";

export function toggleColumnVisibility(columns: readonly CollectionColumnPreference[], key: string): CollectionColumnPreference[] {
  return columns.map((column) => column.key === key ? { ...column, visible: !column.visible } : column);
}
