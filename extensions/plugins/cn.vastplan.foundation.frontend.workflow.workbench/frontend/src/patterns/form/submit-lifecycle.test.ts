import { describe, expect, it, vi } from "vitest";
import type { WorkbenchFormDefinition } from "@vastplan/workbench-sdk";
import { runAfterSubmit, submitFormDefinition, submitValidatedFormDefinition } from "./submit-lifecycle.js";

const base = { id: "edit", schema: { id: "edit", schema: {} }, workflow: { surface: "dialog", title: "Edit" } } as const;

describe("form submit lifecycle", () => {
  it("runs before, submit and after in order while forwarding normalized data", async () => {
    const calls: string[] = [];
    const definition: WorkbenchFormDefinition = {
      ...base,
      async beforeSubmit({ value }) { calls.push("before"); return { value: { ...value, normalized: true } }; },
      async submit({ value }) { calls.push(`submit:${String(value.normalized)}`); return { data: { revision: 2 } }; },
      async afterSubmit({ result }) { calls.push(`after:${String((result?.data as { revision?: number } | undefined)?.revision)}`); },
    };
    const signal = new AbortController().signal;
    const outcome = await submitFormDefinition(definition, { value: { name: "A" }, selected: [], context: {} }, signal);
    expect(outcome.kind).toBe("submitted");
    if (outcome.kind === "submitted") await runAfterSubmit(definition, outcome, signal);
    expect(calls).toEqual(["before", "submit:true", "after:2"]);
  });

  it("stops before persistence when the before hook returns field errors", async () => {
    let submitted = false;
    const definition: WorkbenchFormDefinition = { ...base, async beforeSubmit() { return { fieldErrors: { "/name": "Required" } }; }, async submit() { submitted = true; } };
    const outcome = await submitFormDefinition(definition, { value: {}, selected: [], context: {} }, new AbortController().signal);
    expect(outcome).toMatchObject({ kind: "field-errors", fieldErrors: { "/name": "Required" } });
    expect(submitted).toBe(false);
  });

  it("runs custom validation for the whole form before persistence", async () => {
    let submitted = false;
    const definition: WorkbenchFormDefinition = { ...base,
      async validate() { return { "/name": "Required" }; },
      async submit() { submitted = true; },
    };
    const outcome = await submitFormDefinition(definition, { value: {}, selected: [], context: {} }, new AbortController().signal);
    expect(outcome).toMatchObject({ kind: "field-errors", fieldErrors: { "/name": "Required" } });
    expect(submitted).toBe(false);
  });

  it("does not repeat custom validation after Workbench completed intent validation", async () => {
    const validate = vi.fn(async () => ({}));
    const submit = vi.fn(async () => undefined);
    const definition: WorkbenchFormDefinition = { ...base, validate, submit };
    const outcome = await submitValidatedFormDefinition(definition, { value: { name: "A" }, selected: [], context: {} }, new AbortController().signal);
    expect(outcome.kind).toBe("submitted");
    expect(validate).not.toHaveBeenCalled();
    expect(submit).toHaveBeenCalledOnce();
  });
});
