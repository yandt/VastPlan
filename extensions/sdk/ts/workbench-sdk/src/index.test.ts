import { describe, expect, it } from "vitest";
import { defineCollectionPage, defineMasterDetailPage, defineRecordDetailPage, defineTreeDetailPage, defineWorkspacePage } from "./index.js";
import type { ActionSpec, PageActionSpec } from "./index.js";

describe("defineCollectionPage", () => {
  it("validates the governed table virtualization policy", () => {
    const base = {
      id: "virtual-table",
      path: "/virtual-table",
      title: "Virtual table",
      collection: { id: "items", title: "Items", view: "table" as const, query: { mode: "page" as const, defaultPageSize: 20, pageSizeOptions: [20] }, columns: [] },
      load: async () => ({ items: [], total: 0 }),
    };
    expect(() => defineCollectionPage({ ...base, collection: { ...base.collection, table: { virtualization: "always" as const } } })).not.toThrow();
    expect(() => defineCollectionPage({ ...base, collection: { ...base.collection, table: { virtualization: "invalid" as "auto" } } })).toThrow("虚拟化策略无效");
    expect(() => defineCollectionPage({ ...base, collection: { ...base.collection, view: "cards", query: { ...base.collection.query, mode: "cursor" }, card: { titleKey: "name" }, table: { virtualization: "off" } } })).toThrow("只有 Table 视图");
  });

  it("keeps the serializable collection contract and runtime loader together without exposing a component", async () => {
    const page = defineCollectionPage({
      id: "revisions", path: "/revisions", title: "Revisions",
      collection: { id: "revisions", title: "Revisions", view: "table", query: { mode: "page", defaultPageSize: 20, pageSizeOptions: [20] }, columns: [{ key: "id", label: "ID" }] },
      async load() { return { items: [], total: 0 }; },
    });
    expect(Object.isFrozen(page)).toBe(true);
    await expect(page.load({ mode: "page", page: 1, pageSize: 20, filters: {} }, new AbortController().signal)).resolves.toEqual({ items: [], total: 0 });
  });

  it("requires card collections to use the shared cursor contract", () => {
    expect(() => defineCollectionPage({
      id: "cards", path: "/cards", title: "Cards",
      collection: { id: "cards", title: "Cards", view: "cards", query: { mode: "page", defaultPageSize: 20, pageSizeOptions: [20] }, columns: [] },
      async load() { return { items: [] }; },
    })).toThrow("cursor");
    expect(() => defineCollectionPage({
      id: "cards", path: "/cards", title: "Cards",
      collection: { id: "cards", title: "Cards", view: "cards", query: { mode: "cursor", defaultPageSize: 20, pageSizeOptions: [20] }, columns: [] },
      async load() { return { items: [] }; },
    })).toThrow("card");
  });

  it("validates the reusable FilterPanel contract independently from the collection view", () => {
    const base = {
      id: "revisions", path: "/revisions", title: "Revisions",
      collection: { id: "revisions", title: "Revisions", view: "table" as const, query: { mode: "page" as const, defaultPageSize: 20, pageSizeOptions: [20] }, columns: [{ key: "id", label: "ID" }] },
      async load() { return { items: [], total: 0 }; },
    };
    expect(() => defineCollectionPage({ ...base, collection: { ...base.collection, filterPanel: { fields: [{ id: "status", label: "Status", kind: "select", options: [] }] } } })).not.toThrow();
    expect(() => defineCollectionPage({ ...base, collection: { ...base.collection, filterPanel: { fields: [] } } })).toThrow("FilterPanel");
    expect(() => defineCollectionPage({ ...base, collection: { ...base.collection, filterPanel: { fields: [{ id: "status", label: "One", kind: "text" }, { id: "status", label: "Two", kind: "text" }] } } })).toThrow("重复");
  });

  it("rejects collection actions that escape the governed form registry", () => {
    expect(() => defineCollectionPage({
      id: "connections", path: "/connections", title: "Connections",
      collection: { id: "connections", title: "Connections", view: "table", query: { mode: "page", defaultPageSize: 20, pageSizeOptions: [20] }, columns: [], actions: [{ id: "edit", label: "Edit", icon: "edit", placement: "record.row", form: "edit" }] },
      async load() { return { items: [], total: 0 }; },
    })).toThrow("未声明的表单");
  });

  it("requires every action placement to declare a semantic icon", () => {
    expect(() => defineCollectionPage({
      id: "connections", path: "/connections", title: "Connections",
      collection: { id: "connections", title: "Connections", view: "table", query: { mode: "page", defaultPageSize: 20, pageSizeOptions: [20] }, columns: [], actions: [{ id: "edit", label: "Edit", placement: "record.row" } as unknown as ActionSpec] },
      async load() { return { items: [], total: 0 }; },
    })).toThrow("语义图标");
  });

  it("requires executable actions to use the page-level runAction workflow port", () => {
    expect(() => defineCollectionPage({
      id: "connections", path: "/connections", title: "Connections",
      collection: { id: "connections", title: "Connections", view: "table", query: { mode: "page", defaultPageSize: 20, pageSizeOptions: [20] }, columns: [], actions: [{ id: "refresh", label: "Refresh", icon: "refresh", placement: "collection.toolbar" }] },
      async load() { return { items: [], total: 0 }; },
    })).toThrow("runAction");
  });

  it("keeps page commands independent from collection selection and allows bounded display modes", () => {
    const base = {
      id: "connections", path: "/connections", title: "Connections",
      collection: { id: "connections", title: "Connections", view: "table" as const, query: { mode: "page" as const, defaultPageSize: 20, pageSizeOptions: [20] }, columns: [] },
      async load() { return { items: [], total: 0 }; },
      async runPageAction() {},
    };
    expect(() => defineCollectionPage({ ...base, pageActions: [
      { id: "refresh", label: "Refresh", icon: "refresh" as const },
      { id: "export", label: "Export", icon: "download" as const, display: "icon-label" as const, overflow: "never" as const },
    ] })).not.toThrow();
    expect(() => defineCollectionPage({ ...base, pageActions: [
      { id: "bad", label: "Bad", icon: "warning", requiresSelection: true } as unknown as PageActionSpec,
    ] })).toThrow("不受支持的字段");
  });

  it("requires credentialRef presentation to remain a reference-only schema field", () => {
    expect(() => defineCollectionPage({
      id: "connections", path: "/connections", title: "Connections",
      collection: { id: "connections", title: "Connections", view: "table", query: { mode: "page", defaultPageSize: 20, pageSizeOptions: [20] }, columns: [] },
      pageActions: [{ id: "new", label: "New", icon: "add", form: "new" }],
      forms: [{
        id: "new",
        schema: { id: "new", schema: { type: "object", properties: { credential: { type: "string" } } } },
        presentation: { fields: [{ pointer: "/credential", widget: "credentialRef" }] },
        workflow: { surface: "dialog", title: "New" },
        async submit() {},
      }],
      async load() { return { items: [], total: 0 }; },
    })).toThrow("writeOnly");
  });

  it("accepts one-time secret material only for an uninitialized write-only field", () => {
    const definition = {
      id: "credentials", path: "/credentials", title: "Credentials",
      collection: { id: "credentials", title: "Credentials", view: "table" as const, query: { mode: "page" as const, defaultPageSize: 20, pageSizeOptions: [20] }, columns: [] },
      pageActions: [{ id: "new", label: "New", icon: "add" as const, form: "new" }],
      forms: [{
        id: "new",
        schema: { id: "new", schema: { type: "object", properties: { value: { type: "string", format: "vastplan-secret-material", writeOnly: true } } } },
        presentation: { fields: [{ pointer: "/value", widget: "secretMaterial" as const }] },
        workflow: { surface: "dialog" as const, title: "New" },
        async submit() {},
      }],
      async load() { return { items: [], total: 0 }; },
    };
    expect(() => defineCollectionPage(definition)).not.toThrow();
    expect(() => defineCollectionPage({ ...definition, forms: [{ ...definition.forms[0]!, initialValue: { value: "must-not-be-retained" } }] })).toThrow("initialValue");
  });

  it("accepts duration units only for numeric duration fields", () => {
    const form = {
      id: "timeout",
      schema: { id: "timeout", schema: { type: "object", properties: { timeoutMs: { type: "integer" } } } },
      presentation: { fields: [{ pointer: "/timeoutMs", widget: "duration" as const, duration: { storageUnit: "millisecond" as const, units: ["millisecond", "second", "minute"] as const, defaultUnit: "second" as const } }] },
      workflow: { title: "Timeout" },
      async submit() {},
    };
    const definition = {
      id: "timeouts", path: "/timeouts", title: "Timeouts",
      collection: { id: "timeouts", title: "Timeouts", view: "table" as const, query: { mode: "page" as const, defaultPageSize: 20, pageSizeOptions: [20] }, columns: [] },
      pageActions: [{ id: "new", label: "New", icon: "add" as const, form: "timeout" }], forms: [form],
      async load() { return { items: [], total: 0 }; },
    };
    expect(() => defineCollectionPage(definition)).not.toThrow();
    expect(() => defineCollectionPage({ ...definition, forms: [{ ...form, presentation: { fields: [{ pointer: "/timeoutMs", widget: "duration" as const }] } }] })).toThrow("单位配置无效");
    expect(() => defineCollectionPage({ ...definition, forms: [{ ...form, schema: { id: "timeout", schema: { type: "object", properties: { timeoutMs: { type: "string" } } } } }] })).toThrow("number 或 integer");
  });

  it("rejects actions that escape the governed overlay registry", () => {
    expect(() => defineCollectionPage({
      id: "revisions", path: "/revisions", title: "Revisions",
      collection: { id: "revisions", title: "Revisions", view: "table", query: { mode: "page", defaultPageSize: 20, pageSizeOptions: [20] }, columns: [{ key: "id", label: "ID" }], actions: [{ id: "audit", label: "Audit", icon: "search", placement: "record.row", overlay: "audit" }] },
      async load() { return { items: [], total: 0 }; },
    })).toThrow("未声明的 Overlay");
  });
});

describe("defineWorkspacePage", () => {
  const section = (id: string) => ({
    id,
    page: {
      id: `page.${id}`, path: `/workspace/${id}`, title: id,
      collection: { id: `collection.${id}`, title: id, view: "table" as const, query: { mode: "page" as const, defaultPageSize: 10, pageSizeOptions: [10] }, columns: [{ key: "id", label: "ID" }] },
      async load() { return { items: [], total: 0 }; },
    },
  });

  it("composes independently validated collections into one routed page", () => {
    const page = defineWorkspacePage({ id: "portal.governance", path: "/portals", title: "Portals", sections: [section("profiles"), section("applications")] });
    expect(Object.isFrozen(page)).toBe(true);
    expect(Object.isFrozen(page.sections)).toBe(true);
    expect(page.sections.map((candidate) => candidate.id)).toEqual(["profiles", "applications"]);
  });

  it("rejects empty and duplicate workspace sections", () => {
    expect(() => defineWorkspacePage({ id: "portal.governance", path: "/portals", title: "Portals", sections: [] })).toThrow("Workspace");
    expect(() => defineWorkspacePage({ id: "portal.governance", path: "/portals", title: "Portals", sections: [section("profiles"), section("profiles")] })).toThrow("重复");
  });
});

describe("record pattern definitions", () => {
  const detail = { titleKey: "name", sections: [{ id: "main", fields: [{ key: "name", label: "Name" }] }] } as const;

  it("accepts the three governed record patterns", () => {
    expect(defineRecordDetailPage({ id: "detail", path: "/detail", title: "Detail", pattern: "record-detail", detail, async load() { return { name: "A" }; } }).pattern).toBe("record-detail");
    expect(defineMasterDetailPage({ id: "master", path: "/master", title: "Master", pattern: "master-detail", detail,
      master: { id: "items", title: "Items", keyField: "id", titleField: "name", query: { mode: "page", defaultPageSize: 20, pageSizeOptions: [20] } },
      async loadMaster() { return { items: [], total: 0 }; }, async loadRecord() { return undefined; },
    }).pattern).toBe("master-detail");
    expect(defineTreeDetailPage({ id: "tree", path: "/tree", title: "Tree", pattern: "tree-detail", detail,
      tree: { id: "items", title: "Items", defaultExpandedDepth: 2 }, async loadTree() { return []; }, async loadRecord() { return undefined; },
    }).pattern).toBe("tree-detail");
  });

  it("rejects duplicate fields, arbitrary action positions and non-page editors", () => {
    expect(() => defineRecordDetailPage({ id: "detail", path: "/detail", title: "Detail", pattern: "record-detail",
      detail: { titleKey: "name", sections: [{ id: "main", fields: [{ key: "name", label: "Name" }, { key: "name", label: "Again" }] }] }, async load() { return undefined; },
    })).toThrow("重复");
    expect(() => defineRecordDetailPage({ id: "detail", path: "/detail", title: "Detail", pattern: "record-detail", detail,
      actions: [{ id: "bad", label: "Bad", icon: "warning", placement: "collection.toolbar" }], async load() { return undefined; },
      async runAction() {},
    })).toThrow("位置");
    expect(() => defineRecordDetailPage({ id: "detail", path: "/detail", title: "Detail", pattern: "record-detail", detail,
      editor: { id: "edit", schema: { id: "edit", schema: { type: "object" } }, workflow: { title: "Edit" }, async submit() {} }, async load() { return undefined; },
    })).toThrow("page surface");
  });
});
