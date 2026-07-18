import { describe, expect, it } from "vitest";
import type { RJSFSchema } from "@rjsf/utils";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { arcoDesignSystem, arcoPortalUIComponents } from "./index";
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
