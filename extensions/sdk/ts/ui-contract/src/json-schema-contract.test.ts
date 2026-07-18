import { describe, expect, it } from "vitest";
import { jsonSchemaDialect } from "./index.js";
import type { FormSchema } from "./index.js";

describe("JSON Schema form contract", () => {
  it("keeps the data schema and presentation hints serializable", () => {
    const form: FormSchema = {
      id: "settings",
      schema: {
        $schema: jsonSchemaDialect,
        type: "object",
        properties: {
          name: { type: "string", minLength: 3, default: "portal" },
          credential: { type: "string", format: "vastplan-credential-ref", writeOnly: true },
        },
        required: ["name"],
      },
      uiSchema: {
        name: { "ui:help": "3 个字符以上" },
        credential: { "ui:widget": "secretRef" },
      },
    };

    expect(JSON.parse(JSON.stringify(form))).toEqual(form);
    expect(form.schema.$schema).toBe(jsonSchemaDialect);
  });
});
