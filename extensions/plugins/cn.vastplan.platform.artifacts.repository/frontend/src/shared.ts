import { message, type CollectionQuery } from "@vastplan/workbench-sdk";

export const namespace = "cn.vastplan.platform.artifacts.repository";
export type Row = Record<string, unknown>;

export const text = (key: string, fallback: string) => message(namespace, key, fallback);
export const targetOptions = ["backend", "frontend", "runner", "mobile"].map((value) => ({ value, label: text(`target.${value}`, value) }));
export const lifecycleOptions = ["active", "deprecated", "yanked", "revoked"].map((value) => ({ value, label: text(`lifecycle.${value}`, value) }));

export function filterString(query: CollectionQuery, key: string): string {
  const value = query.filters[key];
  return typeof value === "string" ? value.trim() : "";
}

export function paged(items: readonly Row[], query: CollectionQuery) {
  const start = (query.page - 1) * query.pageSize;
  return { items: items.slice(start, start + query.pageSize), total: items.length };
}

export function formatBytes(value: number): string {
  if (!Number.isFinite(value) || value < 0) return "-";
  const units = ["B", "KiB", "MiB", "GiB", "TiB"];
  let amount = value;
  let unit = 0;
  while (amount >= 1024 && unit < units.length - 1) {
    amount /= 1024;
    unit++;
  }
  return `${amount >= 10 || unit === 0 ? amount.toFixed(0) : amount.toFixed(1)} ${units[unit]}`;
}
