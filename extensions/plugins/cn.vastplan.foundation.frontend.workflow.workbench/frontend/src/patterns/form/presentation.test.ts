import { describe, expect, it } from "vitest";
import { evaluateFormCondition, projectFormPresentation } from "./presentation.js";

describe("FormPresentation", () => {
  it("evaluates the bounded condition DSL against values and read-only context", () => {
    const value = { mode: "advanced", enabled: true };
    expect(evaluateFormCondition({ all: [{ pointer: "/mode", equals: "advanced" }, { pointer: "/enabled", exists: true }] }, value)).toBe(true);
    expect(evaluateFormCondition({ pointer: "/context/role", in: ["admin"] }, value, { role: "admin" })).toBe(true);
    expect(evaluateFormCondition({ not: { pointer: "/enabled", equals: false } }, value)).toBe(true);
  });

  it("projects presentation hints without changing the validation schema", () => {
    const schema = { id: "connection", schema: { $schema: "http://json-schema.org/draft-07/schema#", type: "object", properties: { password: { type: "string" }, notes: { type: "string" } } } } as const;
    const projected = projectFormPresentation(schema, { fields: [
      { pointer: "/password", widget: "credentialRef", visibleWhen: { pointer: "/mode", equals: "secret" } },
      { pointer: "/notes", widget: "textarea", span: 2, help: "Notes help" },
    ] }, { mode: "plain" }, {}, String);
    expect(projected.schema).toBe(schema.schema);
    expect(projected.uiSchema).toMatchObject({ password: { "ui:widget": "hidden" }, notes: { "ui:widget": "textarea", "ui:help": "Notes help", "ui:options": { vastplanSpan: 2 } } });
  });
});
