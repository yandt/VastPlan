import { describe, expect, it, vi } from "vitest";
import type { FormRendererValidationState } from "@vastplan/ui-primitives";
import type { WorkbenchFormDefinition } from "@vastplan/workbench-sdk";
import { formValidationReady, validateFormIntent } from "./form-validation.js";

const base = { id: "edit", schema: { id: "edit", schema: {} }, workflow: { surface: "dialog", title: "Edit" }, async submit() {} } as const;
const context = { value: { name: "current" }, selected: [], context: {} };

function state(overrides: Partial<FormRendererValidationState> = {}): FormRendererValidationState {
  return { valid: true, validating: false, issues: [], errors: {}, ...overrides };
}

describe("form intent validation", () => {
  it("distinguishes the initial renderer snapshot from an in-flight blur validation", () => {
    expect(formValidationReady(state({ valid: false }))).toBe(false);
    expect(formValidationReady(state({ valid: false, validating: true }))).toBe(true);
  });

  it("freshly validates the current value instead of rejecting the blur validation state", async () => {
    const validate = vi.fn(async () => ({}));
    const definition: WorkbenchFormDefinition = { ...base, validate };
    const outcome = await validateFormIntent(definition, context, state({ valid: false, validating: true }), new AbortController().signal);
    expect(outcome).toEqual({ kind: "valid" });
    expect(validate).toHaveBeenCalledWith({ value: context.value, context: {}, signal: expect.any(AbortSignal) });
  });

  it("returns current schema errors before invoking custom validation", async () => {
    const validate = vi.fn(async () => ({}));
    const definition: WorkbenchFormDefinition = { ...base, validate };
    const validation = state({ valid: false, issues: [{ path: "/name", code: "required" }], errors: { "/name": "Required" } });
    const outcome = await validateFormIntent(definition, context, validation, new AbortController().signal);
    expect(outcome).toEqual({ kind: "field-errors", fieldErrors: { "/name": "Required" } });
    expect(validate).not.toHaveBeenCalled();
  });

  it("returns fresh custom errors and observes cancellation", async () => {
    const definition: WorkbenchFormDefinition = { ...base, async validate() { return { "/name": "Unavailable" }; } };
    await expect(validateFormIntent(definition, context, state(), new AbortController().signal))
      .resolves.toEqual({ kind: "field-errors", fieldErrors: { "/name": "Unavailable" } });
    const controller = new AbortController();
    controller.abort();
    await expect(validateFormIntent(definition, context, state(), controller.signal)).resolves.toEqual({ kind: "cancelled" });
  });
});
