import { describe, expect, it } from "vitest";
import { evaluateFormCondition, projectFormPresentation, resolveWorkbenchFormPresentation } from "./presentation.js";

describe("FormPresentation", () => {
  it("keeps every Workbench business form inline unless explicitly overridden", () => {
    expect(resolveWorkbenchFormPresentation(undefined)).toMatchObject({ labelPlacement: "inline", controlAlignment: "end" });
    expect(resolveWorkbenchFormPresentation({ layout: "vertical" }).labelPlacement).toBe("inline");
    expect(resolveWorkbenchFormPresentation({ preset: "guided" }).labelPlacement).toBe("inline");
    expect(resolveWorkbenchFormPresentation({ labelPlacement: "stacked" }).labelPlacement).toBe("stacked");
    expect(resolveWorkbenchFormPresentation({ layout: "compact", labelPlacement: "inside-inline" }).labelPlacement).toBe("inside-inline");
    expect(resolveWorkbenchFormPresentation({ controlAlignment: "start" }).controlAlignment).toBe("start");
  });

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

  it("projects conditional widgets at nested JSON pointers", () => {
    const schema = { id: "database", schema: { type: "object", properties: { providerId: { type: "string" }, options: { type: "object", properties: { applicationName: { type: "string" }, network: { type: "string" } } } } } } as const;
    const presentation = { fields: [
      { pointer: "/options/applicationName", visibleWhen: { pointer: "/providerId", equals: "postgresql" as const } },
      { pointer: "/options/network", visibleWhen: { pointer: "/providerId", equals: "mysql" as const } },
    ] };
    expect(projectFormPresentation(schema, presentation, { providerId: "postgresql" }, {}, String).uiSchema).toMatchObject({ options: { network: { "ui:widget": "hidden" } } });
    expect(projectFormPresentation(schema, presentation, { providerId: "mysql" }, {}, String).uiSchema).toMatchObject({ options: { applicationName: { "ui:widget": "hidden" } } });
  });

  it("projects governed duration units without changing the numeric schema", () => {
    const schema = { id: "timeouts", schema: { type: "object", properties: { timeoutMs: { type: "integer" } } } } as const;
    const projected = projectFormPresentation(schema, { fields: [{ pointer: "/timeoutMs", widget: "duration", duration: { storageUnit: "millisecond", units: ["millisecond", "second", "minute"], defaultUnit: "second" } }] }, {}, {}, String);
    expect(projected.schema).toBe(schema.schema);
    expect(projected.uiSchema).toEqual({ timeoutMs: { "ui:widget": "duration", "ui:options": { vastplanDuration: { storageUnit: "millisecond", units: ["millisecond", "second", "minute"], defaultUnit: "second" } } } });
  });

  it("lets a FormDialog section own the title of its direct object field", () => {
    const schema = { id: "database", schema: { type: "object", properties: { options: { type: "object", title: "连接选项", properties: { user: { type: "string", title: "用户名" } } } } } } as const;
    const presentation = { navigation: "sections" as const, sections: [{ id: "options", title: "Provider 连接选项", fields: ["/options"] }] };

    const projected = projectFormPresentation(schema, presentation, {}, {}, String);

    expect((projected.schema.properties as Record<string, Record<string, unknown>>).options).not.toHaveProperty("title");
    expect((schema.schema.properties.options as Record<string, unknown>)).toHaveProperty("title", "连接选项");
  });
});
