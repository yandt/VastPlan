import type { FilterSpec } from "@vastplan/ui-contract";
import { jsonSchemaDialect, type FormSchema } from "@vastplan/ui-primitives";

/** Converts the governed filter declaration into the shared JSON Schema renderer input. */
export function collectionFilterSchema(filters: readonly FilterSpec[]): FormSchema {
  const properties = Object.fromEntries(filters.map((filter) => [filter.id, { ...filterProperty(filter), title: filter.id }]));
  const localization = Object.fromEntries(filters.map((filter) => [`/properties/${escapePointer(filter.id)}/title`, filter.label]));
  return {
    id: "workbench.collection.filters",
    schema: { $schema: jsonSchemaDialect, type: "object", properties },
    localization,
  } as unknown as FormSchema;
}

function escapePointer(value: string): string { return value.replace(/~/g, "~0").replace(/\//g, "~1"); }

function filterProperty(filter: FilterSpec) {
  if (filter.kind === "select") return { type: "string", oneOf: (filter.options ?? []).map((option) => ({ const: option.value, title: option.value })) };
  if (filter.kind === "boolean") return { type: "boolean" };
  if (filter.kind === "numberRange") return { type: "object", properties: { from: { type: "number" }, to: { type: "number" } } };
  if (filter.kind === "dateRange") return { type: "object", properties: { from: { type: "string", format: "date" }, to: { type: "string", format: "date" } } };
  return { type: "string" };
}
