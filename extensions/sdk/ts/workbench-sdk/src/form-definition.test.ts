import { describe, expect, it } from "vitest";
import { defineCollectionPage, validateFormPresentation, type CollectionPageDefinition, type WorkbenchFormDefinition } from "./index.js";

const schema = { id: "profile", schema: { $schema: "http://json-schema.org/draft-07/schema#", type: "object", properties: { name: { type: "string" }, endpoint: { type: "string" } } } } as const;

function page(form: WorkbenchFormDefinition): CollectionPageDefinition {
  return {
    id: "profiles", path: "/profiles", title: "Profiles",
    collection: { id: "profiles", title: "Profiles", view: "table", query: { mode: "page", defaultPageSize: 10, pageSizeOptions: [10] }, columns: [] },
    forms: [form], async load() { return { items: [], total: 0 }; },
  };
}

describe("Workbench form definition", () => {
  it("accepts data-driven presets, percentage columns and submit hooks", () => {
    const form: WorkbenchFormDefinition = {
      id: "edit", schema,
      presentation: { preset: "standard", labelPlacement: "inline", columns: 2, columnWidths: [35, 65] },
      workflow: { surface: "dialog", title: "Edit" },
      async beforeSubmit({ value }) { return { value: { ...value, normalized: true } }; },
      async submit() { return { data: { revision: 2 } }; },
      async afterSubmit() { /* application callback */ },
    };
    expect(defineCollectionPage(page(form)).forms?.[0]?.presentation).toMatchObject({ columns: 2, columnWidths: [35, 65] });
  });

  it("rejects invalid percentage column definitions", () => {
    const form: WorkbenchFormDefinition = { id: "edit", schema, presentation: { columns: 2, columnWidths: [30, 60] }, workflow: { surface: "dialog", title: "Edit" }, async submit() {} };
    expect(() => defineCollectionPage(page(form))).toThrow("合计 100");
    expect(() => validateFormPresentation({ columns: 2, columnWidths: [20, 80, 0] }, "prepared")).toThrow("匹配列数");
  });
});
