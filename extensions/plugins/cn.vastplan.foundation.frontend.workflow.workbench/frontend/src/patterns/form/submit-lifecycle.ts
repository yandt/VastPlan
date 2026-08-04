import type { WorkbenchFormDefinition, WorkbenchFormSubmitContext, WorkbenchFormSubmitResult } from "@vastplan/workbench-sdk";

export type FormSubmitOutcome =
  | { kind: "cancelled" }
  | { kind: "field-errors"; fieldErrors: NonNullable<WorkbenchFormSubmitResult["fieldErrors"]> }
  | { kind: "submitted"; context: WorkbenchFormSubmitContext; result?: WorkbenchFormSubmitResult };

export async function submitFormDefinition(definition: WorkbenchFormDefinition, context: WorkbenchFormSubmitContext, signal: AbortSignal): Promise<FormSubmitOutcome> {
  // 直接调用保留完整提交生命周期；Workbench 内部可复用已经完成的意图校验。
  const validation = await definition.validate?.({ value: context.value, context: context.context ?? {}, signal });
  if (signal.aborted) return { kind: "cancelled" };
  if (validation !== undefined && Object.keys(validation).length > 0) return { kind: "field-errors", fieldErrors: validation };
  return persistFormDefinition(definition, context, signal);
}

/** Workbench 已校验同一份值后进入持久化阶段，避免自定义校验重复执行。 */
export async function submitValidatedFormDefinition(definition: WorkbenchFormDefinition, context: WorkbenchFormSubmitContext, signal: AbortSignal): Promise<FormSubmitOutcome> {
  return persistFormDefinition(definition, context, signal);
}

async function persistFormDefinition(definition: WorkbenchFormDefinition, context: WorkbenchFormSubmitContext, signal: AbortSignal): Promise<FormSubmitOutcome> {
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
