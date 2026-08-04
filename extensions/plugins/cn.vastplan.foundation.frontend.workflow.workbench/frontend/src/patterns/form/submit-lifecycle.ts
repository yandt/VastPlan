import type { WorkbenchFormDefinition, WorkbenchFormSubmitContext, WorkbenchFormSubmitResult } from "@vastplan/workbench-sdk";

export type FormSubmitOutcome =
  | { kind: "cancelled" }
  | { kind: "field-errors"; fieldErrors: NonNullable<WorkbenchFormSubmitResult["fieldErrors"]> }
  | { kind: "submitted"; context: WorkbenchFormSubmitContext; result?: WorkbenchFormSubmitResult };

export async function submitFormDefinition(definition: WorkbenchFormDefinition, context: WorkbenchFormSubmitContext, signal: AbortSignal): Promise<FormSubmitOutcome> {
  // 自定义校验与 Schema 校验一样只在失焦或提交时运行；提交路径必须覆盖整表。
  const validation = await definition.validate?.({ value: context.value, context: context.context ?? {}, signal });
  if (signal.aborted) return { kind: "cancelled" };
  if (validation !== undefined && Object.keys(validation).length > 0) return { kind: "field-errors", fieldErrors: validation };
  const before = await definition.beforeSubmit?.(context, signal);
  if (signal.aborted || before?.cancelled === true) return { kind: "cancelled" };
  if (before?.fieldErrors !== undefined && Object.keys(before.fieldErrors).length > 0) return { kind: "field-errors", fieldErrors: before.fieldErrors };
  const submittedContext = { ...context, value: { ...(before?.value ?? context.value) } };
  const result = await definition.submit(submittedContext, signal);
  if (signal.aborted) return { kind: "cancelled" };
  if (result?.fieldErrors !== undefined && Object.keys(result.fieldErrors).length > 0) return { kind: "field-errors", fieldErrors: result.fieldErrors };
  return { kind: "submitted", context: submittedContext, ...(result === undefined ? {} : { result }) };
}

export async function runAfterSubmit(definition: WorkbenchFormDefinition, outcome: Extract<FormSubmitOutcome, { kind: "submitted" }>, signal: AbortSignal): Promise<void> {
  await definition.afterSubmit?.({ ...outcome.context, ...(outcome.result === undefined ? {} : { result: outcome.result }) }, signal);
}
