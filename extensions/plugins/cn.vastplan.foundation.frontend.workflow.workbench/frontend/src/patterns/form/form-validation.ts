import type { FormRendererValidationState } from "@vastplan/ui-primitives";
import type { WorkbenchFormDefinition, WorkbenchFormFieldErrors, WorkbenchFormSubmitContext } from "@vastplan/workbench-sdk";

export type FormIntentValidationOutcome =
  | { kind: "valid" }
  | { kind: "pending" }
  | { kind: "cancelled" }
  | { kind: "field-errors"; fieldErrors: WorkbenchFormFieldErrors };

/** Renderer 初始快照只有 invalid 占位且没有 issue；失焦校验运行中则已经具备可执行的同步快照。 */
export function formValidationReady(validation: FormRendererValidationState): boolean {
  return validation.valid || validation.validating || validation.issues.length > 0 || Object.keys(validation.errors).length > 0;
}

/** 为提交或受治理辅助动作校验当前值，不复用失焦请求的瞬时结论。 */
export async function validateFormIntent(
  definition: WorkbenchFormDefinition,
  context: WorkbenchFormSubmitContext,
  validation: FormRendererValidationState,
  signal: AbortSignal,
): Promise<FormIntentValidationOutcome> {
  if (signal.aborted) return { kind: "cancelled" };
  if (!formValidationReady(validation)) return { kind: "pending" };
  if (validation.issues.length > 0) return { kind: "field-errors", fieldErrors: validation.errors };

  const fieldErrors = await definition.validate?.({ value: context.value, context: context.context ?? {}, signal });
  if (signal.aborted) return { kind: "cancelled" };
  if (fieldErrors !== undefined && Object.keys(fieldErrors).length > 0) return { kind: "field-errors", fieldErrors };
  return { kind: "valid" };
}
