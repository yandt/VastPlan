import type { CollectionSpec } from "@vastplan/ui-contract";
import type { CollectionPreference } from "@vastplan/ui-primitives";
import type { CollectionColumnPreference } from "./model.js";

function initialColumns(collection: CollectionSpec): CollectionColumnPreference[] {
  return collection.columns.map((column) => ({ key: column.key, visible: column.defaultVisible !== false }));
}

function preferencesKey(scope: string, collection: CollectionSpec): string {
  return `vastplan.workbench.columns.${scope}.${collection.id}`;
}

export function readCollectionColumns(scope: string, collection: CollectionSpec, preference?: CollectionPreference): CollectionColumnPreference[] {
  const fallback = initialColumns(collection);
  if (preference?.columns !== undefined || preference?.hiddenColumns !== undefined) {
    return restoreColumns(collection, fallback, preference.columns, preference.hiddenColumns);
  }
  try {
    const raw = globalThis.localStorage?.getItem(preferencesKey(scope, collection));
    if (raw === null || raw === undefined) return fallback;
    const parsed = JSON.parse(raw) as unknown;
    if (!Array.isArray(parsed)) return fallback;
    const order = parsed.flatMap((item) => typeof item === "object" && item !== null && typeof (item as { key?: unknown }).key === "string" ? [(item as { key: string }).key] : []);
    const hidden = parsed.flatMap((item) => typeof item === "object" && item !== null && typeof (item as { key?: unknown }).key === "string" && (item as { visible?: unknown }).visible === false ? [(item as { key: string }).key] : []);
    return restoreColumns(collection, fallback, order, hidden);
  } catch { return fallback; }
}

export function writeCollectionColumns(scope: string, collection: CollectionSpec, columns: readonly CollectionColumnPreference[]): void {
  try { globalThis.localStorage?.setItem(preferencesKey(scope, collection), JSON.stringify(columns)); } catch { /* Browser privacy mode may reject local preference storage. */ }
}

export function collectionPreferenceFromColumns(columns: readonly CollectionColumnPreference[], base: CollectionPreference = {}): CollectionPreference {
  return Object.freeze({
    ...base,
    columns: Object.freeze(columns.map((column) => column.key)),
    hiddenColumns: Object.freeze(columns.filter((column) => !column.visible).map((column) => column.key)),
  });
}

function restoreColumns(collection: CollectionSpec, fallback: readonly CollectionColumnPreference[], order: readonly string[] | undefined, hidden: readonly string[] | undefined): CollectionColumnPreference[] {
  const allowed = new Set(collection.preferences?.allowedColumns ?? collection.columns.map((column) => column.key));
  const hiddenSet = new Set((hidden ?? []).filter((key) => allowed.has(key)));
  const restored = [...new Set(order ?? [])].flatMap((key) => allowed.has(key) ? [{ key, visible: !hiddenSet.has(key) }] : []);
  const missing = fallback.filter((column) => allowed.has(column.key) && !restored.some((item) => item.key === column.key)).map((column) => ({ ...column, visible: hiddenSet.has(column.key) ? false : column.visible }));
  return [...restored, ...missing];
}

export function moveItem<T>(items: readonly T[], index: number, offset: number): T[] {
  const target = index + offset;
  if (target < 0 || target >= items.length) return [...items];
  const copy = [...items];
  const [item] = copy.splice(index, 1);
  copy.splice(target, 0, item!);
  return copy;
}
