import type { FilterSpec } from "@vastplan/ui-contract";
import { jsonSchemaDialect, type FormSchema } from "@vastplan/ui-primitives";

/** Converts the governed filter declaration into the shared JSON Schema renderer input. */
export function collectionFilterSchema(filters: readonly FilterSpec[]): FormSchema {
  return {
    id: "workbench.collection.filters",
    schema: { $schema: jsonSchemaDialect, type: "object", properties: Object.fromEntries(filters.map((filter) => [filter.id, filterProperty(filter)])) },
  } as unknown as FormSchema;
}

function filterProperty(filter: FilterSpec) {
  if (filter.kind === "select") return { type: "string", oneOf: (filter.options ?? []).map((option) => ({ const: option.value, title: option.value })) };
  if (filter.kind === "boolean") return { type: "boolean" };
  if (filter.kind === "numberRange") return { type: "object", properties: { from: { type: "number" }, to: { type: "number" } } };
  if (filter.kind === "dateRange") return { type: "object", properties: { from: { type: "string", format: "date" }, to: { type: "string", format: "date" } } };
  return { type: "string" };
}
