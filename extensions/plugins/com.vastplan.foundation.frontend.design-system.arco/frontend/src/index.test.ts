import { describe, expect, it } from "vitest";
import type { RJSFSchema } from "@rjsf/utils";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { arcoDesignSystem, arcoPortalUIComponents, cascadeResponsiveColumns } from "./index";
import { arcoJSONSchemaValidator, transformArcoFormErrors } from "./json-schema-form";

describe("Arco portal UI adapter", () => {
  it("implements the complete stable component surface", () => {
    expect(Object.keys(arcoPortalUIComponents).sort()).toEqual([
      "Breadcrumb", "Busy", "Button", "CommandPalette", "Descriptions", "Dialog", "Divider", "Drawer",
      "EmptyState", "ErrorState", "FilterBar", "FormRenderer", "Grid", "GridItem", "Icon", "Menu", "Page", "Pagination",
      "Panel", "PortalShell", "Skeleton", "Stack", "Status", "Table", "Tabs", "theme",
    ].sort());
  });

  it("declares every capability implemented by the adapter", () => {
    expect(arcoDesignSystem.capabilities).toEqual(expect.arrayContaining([
      "layout", "menu", "navigation", "overlay", "form", "data", "feedback", "theme",
    ]));
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
    const html = renderToStaticMarkup(createElement(arcoPortalUIComponents.FormRenderer, {
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
    }));

    expect(html).toContain("门户设置");
    expect(html).toContain("名称");
    expect(html).not.toContain("Submit");
  });
});
