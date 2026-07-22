import { describe, expect, it, vi } from "vitest";
import type { RJSFSchema } from "@rjsf/utils";
import { cspJSONSchemaValidator } from "./index.js";

describe("CSP-safe RJSF validator", () => {
  it("validates Draft 7 and VastPlan credential references without dynamic function generation", () => {
    const schema: RJSFSchema = {
      $schema: "http://json-schema.org/draft-07/schema#",
      type: "object",
      required: ["name", "credential"],
      properties: {
        name: { type: "string", minLength: 3 },
        credential: { type: "string", format: "vastplan-credential-ref", writeOnly: true },
      },
    };
    const functionConstructor = vi.spyOn(globalThis, "Function");
    try {
      const invalid = cspJSONSchemaValidator.validateFormData({ name: "x", credential: "plaintext" }, schema);
      expect(invalid.errors.map((error) => error.name)).toEqual(expect.arrayContaining(["minLength", "format"]));
      expect(cspJSONSchemaValidator.validateFormData({ name: "portal", credential: "credential://tenant/key-1" }, schema).errors).toEqual([]);
      expect(functionConstructor).not.toHaveBeenCalled();
    } finally {
      functionConstructor.mockRestore();
    }
  });
});
