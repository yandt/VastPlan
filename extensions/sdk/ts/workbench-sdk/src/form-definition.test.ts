import { describe, expect, it } from "vitest";
import { defineCollectionPage, resolveFormWorkflowSurface, validateFormPresentation, type CollectionPageDefinition, type WorkbenchFormDefinition } from "./index.js";

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
      workflow: { title: "Edit", dialogHeight: 640 },
      async beforeSubmit({ value }) { return { value: { ...value, normalized: true } }; },
      async submit() { return { data: { revision: 2 } }; },
      async afterSubmit() { /* application callback */ },
    };
    expect(defineCollectionPage({ ...page(form), bodyLayout: "medium" })).toMatchObject({ bodyLayout: "medium", forms: [{ workflow: { dialogHeight: 640 } }] });
    expect(resolveFormWorkflowSurface(form.workflow)).toBe("dialog");
  });

  it("rejects arbitrary page body widths", () => {
    expect(() => defineCollectionPage({ ...page({ id: "edit", schema, workflow: { title: "Edit" }, async submit() {} }), bodyLayout: "840px" as never })).toThrow("bodyLayout 无效");
  });

  it("rejects invalid percentage column definitions", () => {
    const form: WorkbenchFormDefinition = { id: "edit", schema, presentation: { columns: 2, columnWidths: [30, 60] }, workflow: { title: "Edit" }, async submit() {} };
    expect(() => defineCollectionPage(page(form))).toThrow("合计 100");
    expect(() => validateFormPresentation({ columns: 2, columnWidths: [20, 80, 0] }, "prepared")).toThrow("匹配列数");
  });

  it("defaults modal forms to dialog and rejects the removed drawer form surface", () => {
    expect(resolveFormWorkflowSurface({ title: "Create" })).toBe("dialog");
    expect(resolveFormWorkflowSurface({ surface: "page", title: "Edit" })).toBe("page");
    expect(() => resolveFormWorkflowSurface({ surface: "drawer", title: "Legacy" } as unknown as Parameters<typeof resolveFormWorkflowSurface>[0])).toThrow("不受支持");
  });

  it("only accepts a bounded pixel height for dialog forms", () => {
    expect(() => defineCollectionPage(page({ id: "edit", schema, workflow: { title: "Edit", dialogHeight: 159 }, async submit() {} }))).toThrow("160..10000");
    expect(() => defineCollectionPage(page({ id: "edit", schema, workflow: { surface: "page", title: "Edit", dialogHeight: 640 }, async submit() {} }))).toThrow("仅可用于 dialog");
  });
});
