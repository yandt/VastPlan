import { describe, expect, it } from "vitest";
import { defineCollectionPage } from "./index.js";

describe("defineCollectionPage", () => {
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

  it("rejects collection actions that escape the governed form registry", () => {
    expect(() => defineCollectionPage({
      id: "connections", path: "/connections", title: "Connections",
      collection: { id: "connections", title: "Connections", view: "table", query: { mode: "page", defaultPageSize: 20, pageSizeOptions: [20] }, columns: [], actions: [{ id: "edit", label: "Edit", placement: "record.row", form: "edit" }] },
      async load() { return { items: [], total: 0 }; },
    })).toThrow("未声明的表单");
  });

  it("requires page actions to declare a semantic icon", () => {
    expect(() => defineCollectionPage({
      id: "connections", path: "/connections", title: "Connections",
      collection: { id: "connections", title: "Connections", view: "table", query: { mode: "page", defaultPageSize: 20, pageSizeOptions: [20] }, columns: [], actions: [{ id: "create", label: "Create", placement: "page.primary" }] },
      async load() { return { items: [], total: 0 }; },
    })).toThrow("语义图标");
  });

  it("requires credentialRef presentation to remain a reference-only schema field", () => {
    expect(() => defineCollectionPage({
      id: "connections", path: "/connections", title: "Connections",
      collection: { id: "connections", title: "Connections", view: "table", query: { mode: "page", defaultPageSize: 20, pageSizeOptions: [20] }, columns: [], actions: [{ id: "new", label: "New", icon: "add", placement: "page.primary", form: "new" }] },
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
      collection: { id: "credentials", title: "Credentials", view: "table" as const, query: { mode: "page" as const, defaultPageSize: 20, pageSizeOptions: [20] }, columns: [], actions: [{ id: "new", label: "New", icon: "add" as const, placement: "page.primary" as const, form: "new" }] },
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

  it("rejects actions that escape the governed overlay registry", () => {
    expect(() => defineCollectionPage({
      id: "revisions", path: "/revisions", title: "Revisions",
      collection: { id: "revisions", title: "Revisions", view: "table", query: { mode: "page", defaultPageSize: 20, pageSizeOptions: [20] }, columns: [{ key: "id", label: "ID" }], actions: [{ id: "audit", label: "Audit", placement: "record.row", overlay: "audit" }] },
      async load() { return { items: [], total: 0 }; },
    })).toThrow("未声明的 Overlay");
  });
});
