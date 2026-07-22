import type { CollectionQuery, CollectionResult } from "@vastplan/workbench-sdk";

export const namespace = "cn.vastplan.platform.configuration.role-management";

export function page<Row extends Record<string, unknown>>(rows: readonly Row[], query: CollectionQuery, matches: (row: Row, text: string) => boolean): CollectionResult<Row> {
  const text = Object.values(query.filters).find((value): value is string => typeof value === "string")?.trim().toLowerCase() ?? "";
  const filtered = text === "" ? [...rows] : rows.filter((row) => matches(row, text));
  const start = Math.max(0, (query.page - 1) * query.pageSize);
  return { items: filtered.slice(start, start + query.pageSize), total: filtered.length };
}
