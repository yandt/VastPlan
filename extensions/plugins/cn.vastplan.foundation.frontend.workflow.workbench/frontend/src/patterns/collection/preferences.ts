import type { CollectionSpec } from "@vastplan/ui-contract";
import type { CollectionColumnPreference } from "./model.js";

function initialColumns(collection: CollectionSpec): CollectionColumnPreference[] {
  return collection.columns.map((column) => ({ key: column.key, visible: column.defaultVisible !== false }));
}

function preferencesKey(scope: string, collection: CollectionSpec): string {
  return `vastplan.workbench.columns.${scope}.${collection.id}`;
}

export function readCollectionColumns(scope: string, collection: CollectionSpec): CollectionColumnPreference[] {
  const fallback = initialColumns(collection);
  try {
    const raw = globalThis.localStorage?.getItem(preferencesKey(scope, collection));
    if (raw === null || raw === undefined) return fallback;
    const parsed = JSON.parse(raw) as unknown;
    if (!Array.isArray(parsed)) return fallback;
    const allowed = new Set(collection.preferences?.allowedColumns ?? collection.columns.map((column) => column.key));
    const restored = parsed.flatMap((item) => typeof item === "object" && item !== null && typeof (item as { key?: unknown }).key === "string" && allowed.has((item as { key: string }).key)
      ? [{ key: (item as { key: string }).key, visible: (item as { visible?: unknown }).visible !== false }]
      : []);
    const missing = fallback.filter((column) => !restored.some((item) => item.key === column.key));
    return [...restored, ...missing];
  } catch { return fallback; }
}

export function writeCollectionColumns(scope: string, collection: CollectionSpec, columns: readonly CollectionColumnPreference[]): void {
  try { globalThis.localStorage?.setItem(preferencesKey(scope, collection), JSON.stringify(columns)); } catch { /* Browser privacy mode may reject local preference storage. */ }
}

export function moveItem<T>(items: readonly T[], index: number, offset: number): T[] {
  const target = index + offset;
  if (target < 0 || target >= items.length) return [...items];
  const copy = [...items];
  const [item] = copy.splice(index, 1);
  copy.splice(target, 0, item!);
  return copy;
}
