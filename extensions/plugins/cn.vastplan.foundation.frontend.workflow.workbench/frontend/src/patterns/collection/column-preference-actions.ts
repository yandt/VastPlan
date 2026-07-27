import type { CollectionColumnPreference } from "./model.js";

export function reorderColumns(columns: readonly CollectionColumnPreference[], sourceKey: string, targetKey: string): CollectionColumnPreference[] {
  const source = columns.findIndex((column) => column.key === sourceKey);
  const target = columns.findIndex((column) => column.key === targetKey);
  if (source < 0 || target < 0 || source === target) return [...columns];
  const next = [...columns];
  const [column] = next.splice(source, 1);
  next.splice(target, 0, column!);
  return next;
}

export function toggleColumnVisibility(columns: readonly CollectionColumnPreference[], key: string): CollectionColumnPreference[] {
  return columns.map((column) => column.key === key ? { ...column, visible: !column.visible } : column);
}
