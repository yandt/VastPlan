import { describe, expect, it } from "vitest";
import { applyFormDefaults, getFormValue, isFormFieldVisible, validateForm } from "./index.js";
import type { FormField, FormSchema } from "./index.js";

const schema: FormSchema = {
  id: "settings",
  fields: [
    { key: "enabled", type: "boolean", title: "启用", defaultValue: true },
    { key: "name", type: "text", title: "名称", validation: { required: true, min: 3, max: 8 } },
    { key: "port", type: "number", title: "端口", validation: { min: 1, max: 65_535 } },
    { key: "path", type: "text", title: "路径", validation: { pattern: "^/" } },
    { key: "advanced", type: "text", title: "高级", visibleWhen: { key: "enabled", equals: true }, validation: { required: true } },
    { key: "connection", type: "object", title: "连接", fields: [
      { key: "host", type: "text", title: "主机", defaultValue: "localhost", validation: { required: true } },
    ] },
    { key: "plugins", type: "array", title: "插件", fields: [
      { key: "id", type: "text", title: "ID", validation: { required: true } },
      { key: "channel", type: "text", title: "通道", defaultValue: "stable" },
    ] },
  ],
};

describe("dynamic form runtime", () => {
  it("applies defaults recursively without mutating the input", () => {
    const input = { plugins: [{}] };
    expect(applyFormDefaults(schema, input)).toEqual({ enabled: true, connection: { host: "localhost" }, plugins: [{ channel: "stable" }] });
    expect(input).toEqual({ plugins: [{}] });
  });

  it("preserves the input identity when no defaults are missing", () => {
    const complete = { enabled: false, name: "ready", port: 10, path: "/", connection: { host: "db" }, plugins: [] };
    expect(applyFormDefaults(schema, complete)).toBe(complete);
  });

  it("resolves nested object and array paths", () => {
    expect(getFormValue({ connection: { host: "db" }, plugins: [{ id: "one" }] }, "connection.host")).toBe("db");
    expect(getFormValue({ plugins: [{ id: "one" }] }, "plugins[0].id")).toBe("one");
  });

  it("evaluates conditions against the root form value", () => {
    const field: FormField = { key: "x", type: "text", title: "X", visibleWhen: { key: "mode", notEquals: "basic" } };
    expect(isFormFieldVisible(field, { mode: "advanced" })).toBe(true);
    expect(isFormFieldVisible(field, { mode: "basic" })).toBe(false);
  });

  it("validates visible scalar and nested fields with stable paths", () => {
    const result = validateForm(schema, { enabled: true, name: "ab", port: 70_000, path: "relative", connection: {}, plugins: [{}] });
    expect(result.valid).toBe(false);
    expect(result.issues.map(({ path, code }) => [path, code])).toEqual([
      ["name", "min"], ["port", "max"], ["path", "pattern"], ["advanced", "required"],
      ["connection.host", "required"], ["plugins[0].id", "required"],
    ]);
  });

  it("does not validate hidden fields and reports invalid patterns safely", () => {
    const invalidPatternSchema: FormSchema = { id: "bad", fields: [{ key: "x", type: "text", title: "X", validation: { pattern: "[" } }] };
    expect(validateForm(schema, { enabled: false, name: "valid", port: 10, path: "/ok", connection: { host: "db" }, plugins: [] }).valid).toBe(true);
    expect(validateForm(invalidPatternSchema, { x: "anything" }).issues[0]?.code).toBe("invalidPattern");
  });
});
