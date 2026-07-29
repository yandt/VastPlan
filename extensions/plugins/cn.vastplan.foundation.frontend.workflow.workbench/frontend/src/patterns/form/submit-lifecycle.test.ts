import { describe, expect, it } from "vitest";
import type { WorkbenchFormDefinition } from "@vastplan/workbench-sdk";
import { runAfterSubmit, submitFormDefinition } from "./submit-lifecycle.js";

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
});
