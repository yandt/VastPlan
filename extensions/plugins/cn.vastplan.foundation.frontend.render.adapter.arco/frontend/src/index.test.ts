import { describe, expect, it } from "vitest";
import type { RJSFSchema } from "@rjsf/utils";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { arcoRenderAdapter, arcoPortalUIComponents, cascadeResponsiveColumns } from "./index";
import { arcoJSONSchemaValidator, transformArcoFormErrors } from "./json-schema-form";
import { PortalI18nProvider } from "@vastplan/ui-primitives";

describe("Arco portal UI adapter", () => {
  it("implements the complete stable component surface", () => {
    expect(Object.keys(arcoPortalUIComponents).sort()).toEqual([
      "Breadcrumb", "Busy", "Button", "CommandPalette", "Descriptions", "Dialog", "Divider", "Drawer",
      "DataCard", "EmptyState", "ErrorState", "FilterBar", "FormRenderer", "Grid", "GridItem", "Icon", "Menu", "Page", "Pagination",
      "Panel", "Popover", "PortalShell", "Skeleton", "Stack", "Status", "Table", "Tabs", "theme",
    ].sort());
  });

  it("declares every capability implemented by the adapter", () => {
    expect(arcoRenderAdapter.capabilities).toEqual(expect.arrayContaining([
      "layout", "menu", "navigation", "overlay", "form", "data", "feedback", "theme",
    ]));
  });

  it("maps the shared shell, overlay, focus and touch baselines", () => {
    expect(arcoPortalUIComponents.theme.tokens).toMatchObject({
      shell: { barHeight: 64, railWidth: 64, navigationWidth: 240, navigationCompactWidth: 220 },
      overlay: { navigationMinWidth: 480, navigationMaxWidth: 840 },
      focus: { width: 2 }, touch: { minimum: 44 }, motion: { fast: 120, normal: 180 },
    });
  });

  it("preserves framework-neutral GridItem children through Arco Grid", () => {
    const html = renderToStaticMarkup(createElement(arcoPortalUIComponents.Grid, {
      columns: 2,
      children: [
        createElement(arcoPortalUIComponents.GridItem, { key: "left", span: 1, children: "left" }),
        createElement(arcoPortalUIComponents.GridItem, { key: "right", span: 1, children: "right" }),
      ],
    }));

    expect(html).toContain("left");
    expect(html).toContain("right");
  });

  it("cascades framework-neutral responsive columns across Arco breakpoints", () => {
    expect(cascadeResponsiveColumns({ xs: 1, lg: 2 })).toEqual({ xs: 1, sm: 1, md: 1, lg: 2, xl: 2 });
    expect(cascadeResponsiveColumns({ sm: 2, xl: 4 })).toEqual({ sm: 2, md: 2, lg: 2, xl: 4 });
    expect(cascadeResponsiveColumns(3)).toBe(3);
  });

  it("keeps wide semantic tables horizontally scrollable on narrow pages", () => {
    const html = renderToStaticMarkup(createElement(arcoPortalUIComponents.Table, {
      columns: [{ key: "name", title: "Name" }, { key: "updatedAt", title: "Updated" }],
      rows: [{ id: "one", name: "Portal", updatedAt: "2026-07-19" }],
    }));
    expect(html).toContain("arco-table-layout-fixed");
    expect(html).toContain("arco-table-content-scroll");
    expect(html).toContain("width:max-content");
  });

  it("maps the semantic data card to native selectable Arco markup", () => {
    const html = renderToStaticMarkup(createElement(arcoPortalUIComponents.DataCard, {
      title: "Node A", subtitle: "linux", status: "Ready", selectable: true, selected: true, selectionLabel: "Select Node A", children: "4 cores",
    }));
    expect(html).toContain("Node A");
    expect(html).toContain("Select Node A");
    expect(html).toContain("arco-card");
    expect(html).toContain("arco-checkbox-checked");
  });

  it("keeps navigation destinations as real links", () => {
    const html = renderToStaticMarkup(createElement(arcoPortalUIComponents.Menu, {
      items: [{ id: "settings", label: "设置", href: "/settings" }],
    }));
    expect(html).toContain('href="/settings"');
  });

  it("validates standard JSON Schema constraints and credential references through AJV", () => {
    const schema: RJSFSchema = {
      $schema: "http://json-schema.org/draft-07/schema#",
      type: "object",
      required: ["name", "credential"],
      properties: {
        name: { type: "string", minLength: 3 },
        credential: { type: "string", format: "vastplan-credential-ref", writeOnly: true },
      },
    };

    const invalid = arcoJSONSchemaValidator.validateFormData({ name: "x", credential: "plaintext" }, schema, undefined, transformArcoFormErrors);
    expect(invalid.errors.map((error) => error.name)).toEqual(expect.arrayContaining(["minLength", "format"]));
    expect(invalid.errors.every((error) => error.message !== undefined)).toBe(true);

    const valid = arcoJSONSchemaValidator.validateFormData({ name: "portal", credential: "credential://tenant/key-1" }, schema);
    expect(valid.errors).toEqual([]);
  });

  it("renders a standard JSON Schema through the Arco theme without a framework submit button", () => {
    const form = createElement(arcoPortalUIComponents.FormRenderer, {
      schema: {
        id: "portal",
        schema: {
          $schema: "http://json-schema.org/draft-07/schema#",
          type: "object",
          title: "门户设置",
          properties: {
            name: { type: "string", title: "名称" },
            enabled: { type: "boolean", title: "启用" },
          },
        },
      },
      value: {},
      onChange: () => undefined,
    });
    const html = renderToStaticMarkup(createElement(PortalI18nProvider, { policy: { defaultLocale: "zh-CN", supportedLocales: ["zh-CN", "en-US"] }, catalogs: { "cn.vastplan.foundation.frontend.render.adapter.arco": arcoRenderAdapter.localization! }, candidates: ["zh-CN"], children: form }));

    expect(html).toContain("门户设置");
    expect(html).toContain("名称");
    expect(html).not.toContain("Submit");
  });

  it("renders governed form sections without splitting the validation schema", () => {
    const form = createElement(arcoPortalUIComponents.FormRenderer, {
      schema: { id: "node", schema: { $schema: "http://json-schema.org/draft-07/schema#", type: "object", properties: { name: { type: "string", title: "Name" }, region: { type: "string", title: "Region" } } } },
      value: {}, onChange: () => undefined,
      presentation: { navigation: "sections", sections: [{ id: "identity", title: "Identity", columns: 2, fields: ["/name", "/region"] }], fields: [{ pointer: "/name", span: 2 }] },
    });
    const html = renderToStaticMarkup(createElement(PortalI18nProvider, { policy: { defaultLocale: "en-US", supportedLocales: ["en-US"] }, catalogs: {}, candidates: ["en-US"], children: form }));
    expect(html).toContain("Identity");
    expect(html).toContain("Name");
    expect(html).toContain("Region");
  });

  it("renders one-time secret material as a non-autofilled password input", () => {
    const form = createElement(arcoPortalUIComponents.FormRenderer, {
      schema: { id: "secret", schema: { $schema: "http://json-schema.org/draft-07/schema#", type: "object", properties: { value: { type: "string", format: "vastplan-secret-material", writeOnly: true } } }, uiSchema: { value: { "ui:widget": "password" } } },
      value: {}, onChange: () => undefined,
    });
    const html = renderToStaticMarkup(createElement(PortalI18nProvider, { policy: { defaultLocale: "en-US", supportedLocales: ["en-US"] }, catalogs: {}, candidates: ["en-US"], children: form }));
    expect(html).toContain('type="password"');
    expect(html).toContain('autoComplete="new-password"');
  });

  it("localizes enum titles addressed through JSON Pointer array indexes", () => {
    const form = createElement(arcoPortalUIComponents.FormRenderer, {
      schema: {
        id: "enum", schema: { $schema: "http://json-schema.org/draft-07/schema#", type: "object", properties: { mode: { type: "string", title: "模式", oneOf: [{ const: "safe", title: "安全" }] } } },
        localization: { "/properties/mode/title": "Mode", "/properties/mode/oneOf/0/title": "Safe" },
      },
      value: { mode: "safe" }, onChange: () => undefined,
    });
    const html = renderToStaticMarkup(createElement(PortalI18nProvider, { policy: { defaultLocale: "en-US", supportedLocales: ["en-US"] }, catalogs: {}, candidates: ["en-US"], children: form }));
    expect(html).toContain("Mode");
    expect(html).toContain("Safe");
    expect(html).not.toContain("安全");
  });
});
