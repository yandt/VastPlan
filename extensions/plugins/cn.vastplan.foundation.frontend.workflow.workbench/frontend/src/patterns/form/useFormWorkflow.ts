import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { formControlSize, message, usePortalI18n, usePortalUI, type FormRendererProps, type FormRendererValidationState } from "@vastplan/ui-primitives";
import { resolveFormWorkflowSurface, validateFormPresentation, type FormActionSpec, type WorkbenchFormDefinition, type WorkbenchFormPreparation } from "@vastplan/workbench-sdk";
import type { CollectionRow } from "../collection/model.js";
import { projectFormPresentation, resolveWorkbenchFormPresentation } from "./presentation.js";
import { containsSecretMaterial, discardSecretMaterial, secretMaterialPointers } from "./secret-material.js";
import { useStableSelection } from "./stable-selection.js";
import { localizeFormFieldErrors } from "./field-errors.js";
import { validateFormIntent } from "./form-validation.js";
import { runAfterSubmit, submitValidatedFormDefinition } from "./submit-lifecycle.js";

const namespace = "cn.vastplan.foundation.frontend.workflow.workbench";
const emptyValidation: FormRendererValidationState = { valid: false, validating: false, issues: [], errors: {} };
const emptyContext: Readonly<Record<string, unknown>> = Object.freeze({});

interface UseFormWorkflowInput {
  definition?: WorkbenchFormDefinition;
  selected: readonly CollectionRow[];
  open: boolean;
  onClose?(): void;
  onRefresh(): void;
  onDirtyChange?(dirty: boolean): void;
}

export interface FormWorkflowController {
  definition?: WorkbenchFormDefinition;
  presentation: ReturnType<typeof resolveWorkbenchFormPresentation>;
  controlSize: ReturnType<typeof formControlSize>;
  schema?: NonNullable<WorkbenchFormDefinition["schema"]>;
  context: Readonly<Record<string, unknown>>;
  value: Record<string, unknown>;
  loading: boolean;
  submitting: boolean;
  runningActionID?: string;
  failure?: string;
  fieldErrors: Readonly<Record<string, string>>;
  validation: FormRendererValidationState;
  activeSection?: string;
  validate?: FormRendererProps["validate"];
  change(value: Record<string, unknown>): void;
  setValidation(value: FormRendererValidationState): void;
  setActiveSection(value: string | undefined): void;
  requestClose(): Promise<void>;
  runAction(action: FormActionSpec): Promise<void>;
  submit(): Promise<void>;
}

export function useFormWorkflow(input: UseFormWorkflowInput): FormWorkflowController {
  const { definition, open, onClose, onRefresh, onDirtyChange } = input;
  const ui = usePortalUI();
  const i18n = usePortalI18n();
  const [value, setValue] = useState<Record<string, unknown>>({});
  const [baseline, setBaseline] = useState("{}");
  const [loading, setLoading] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [runningActionID, setRunningActionID] = useState<string>();
  const [failure, setFailure] = useState<string>();
  const [fieldErrors, setFieldErrors] = useState<Readonly<Record<string, string>>>({});
  const [validation, setValidation] = useState<FormRendererValidationState>(emptyValidation);
  const [activeSection, setActiveSection] = useState<string>();
  const [preparation, setPreparation] = useState<WorkbenchFormPreparation>();
  const loadRef = useRef<AbortController>();
  const submitRef = useRef<AbortController>();
  const actionRef = useRef<AbortController>();
  const stableSelected = useStableSelection(input.selected);
  const presentation = useMemo(() => resolveWorkbenchFormPresentation(preparation?.presentation ?? definition?.presentation), [definition?.presentation, preparation?.presentation]);
  const context = preparation?.context ?? definition?.context ?? emptyContext;
  const secretPointers = useMemo(() => secretMaterialPointers(presentation), [presentation]);

  useEffect(() => {
    actionRef.current?.abort();
    setRunningActionID(undefined);
    if (!open || definition === undefined) {
      setValue({}); setBaseline("{}"); setFieldErrors({}); setFailure(undefined); setPreparation(undefined);
      return;
    }
    loadRef.current?.abort();
    const controller = new AbortController();
    loadRef.current = controller;
    setLoading(true); setFailure(undefined); setFieldErrors({}); setValidation(emptyValidation); setPreparation(undefined);
    void (async () => {
      const prepared = await definition.prepare?.(stableSelected, controller.signal) ?? {};
      if (controller.signal.aborted) return;
      validateFormPresentation(prepared.presentation, definition.id);
      const resolvedPresentation = resolveWorkbenchFormPresentation(prepared.presentation ?? definition.presentation);
      const pointers = secretMaterialPointers(resolvedPresentation);
      const loaded = definition.load === undefined ? prepared.initialValue ?? definition.initialValue ?? {} : await definition.load(stableSelected, controller.signal);
      if (controller.signal.aborted) return;
      if (containsSecretMaterial(loaded, pointers)) setFailure(i18n.text(message(namespace, "form.secretLoadRejected", "一次性秘密字段禁止从存储中回填；已安全丢弃该值。")));
      const next = discardSecretMaterial(loaded, pointers);
      setPreparation(prepared); setActiveSection(resolvedPresentation.sections?.[0]?.id);
      setValue(next); setBaseline(JSON.stringify(next));
    })().catch((error: unknown) => { if (!controller.signal.aborted) setFailure(errorText(error)); })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [definition, i18n.text, open, stableSelected]);

  useEffect(() => () => { submitRef.current?.abort(); actionRef.current?.abort(); }, []);
  const schema = useMemo(() => definition === undefined ? undefined : projectFormPresentation(preparation?.schema ?? definition.schema, presentation, value, context, i18n.text), [context, definition, i18n.text, preparation?.schema, presentation, value]);
  const validate = useMemo<FormRendererProps["validate"]>(() => definition?.validate === undefined ? undefined : async ({ value: next, context: nextContext, signal }) => {
    return localizeFormFieldErrors(await definition.validate!({ value: next, context: nextContext, signal }), i18n.text);
  }, [definition, i18n.text]);
  const dirty = JSON.stringify(discardSecretMaterial(value, secretPointers)) !== baseline || containsSecretMaterial(value, secretPointers);

  useEffect(() => {
    onDirtyChange?.(open && definition !== undefined && dirty);
    return () => onDirtyChange?.(false);
  }, [definition, dirty, onDirtyChange, open]);

  const change = useCallback((next: Record<string, unknown>) => {
    setValue(next); setFieldErrors({}); setFailure(undefined);
  }, []);

  const requestClose = useCallback(async () => {
    if (submitting || runningActionID !== undefined || definition === undefined) return;
    if (dirty && !await ui.confirm({ title: i18n.text(message(namespace, "form.discardTitle", "放弃未保存的修改？")), content: i18n.text(message(namespace, "form.discardContent", "关闭后，本次输入不会保留。")) })) return;
    setFieldErrors({}); setFailure(undefined);
    if (resolveFormWorkflowSurface(definition.workflow) === "page") setValue(JSON.parse(baseline) as Record<string, unknown>);
    else {
      const sanitized = discardSecretMaterial(value, secretPointers);
      setValue(sanitized); setBaseline(JSON.stringify(sanitized)); onClose?.();
    }
  }, [baseline, definition, dirty, i18n.text, onClose, runningActionID, secretPointers, submitting, ui, value]);

  const runAction = useCallback(async (action: FormActionSpec) => {
    if (definition?.runAction === undefined || submitting || runningActionID !== undefined || loading) return;
    actionRef.current?.abort();
    const controller = new AbortController();
    actionRef.current = controller;
    setRunningActionID(action.id); setFailure(undefined); setFieldErrors({});
    try {
      if (action.requiresValid !== false) {
        const validationOutcome = await validateFormIntent(definition, { value, selected: stableSelected, context }, validation, controller.signal);
        if (validationOutcome.kind === "cancelled" || validationOutcome.kind === "pending") return;
        if (validationOutcome.kind === "field-errors") { setFieldErrors(localizeFormFieldErrors(validationOutcome.fieldErrors, i18n.text)); return; }
      }
      if (action.confirm !== undefined && !await ui.confirm({ title: i18n.text(action.label), content: i18n.text(action.confirm) })) return;
      if (controller.signal.aborted) return;
      const result = await definition.runAction({ action, value, selected: stableSelected, context }, controller.signal);
      if (controller.signal.aborted || result === undefined) return;
      if (result.fieldErrors !== undefined) setFieldErrors(localizeFormFieldErrors(result.fieldErrors, i18n.text));
      if (result.notify !== undefined) ui.notify({
        title: i18n.text(result.notify.title),
        ...(result.notify.content === undefined ? {} : { content: i18n.text(result.notify.content) }),
        kind: result.notify.kind ?? "success",
      });
    } catch (error) {
      if (!controller.signal.aborted) setFailure(errorText(error));
    } finally {
      if (!controller.signal.aborted) setRunningActionID(undefined);
    }
  }, [context, definition, i18n.text, loading, runningActionID, stableSelected, submitting, ui, validation, value]);

  const submit = useCallback(async () => {
    if (definition === undefined || submitting || runningActionID !== undefined || loading) return;
    submitRef.current?.abort();
    const controller = new AbortController();
    submitRef.current = controller;
    setSubmitting(true); setFailure(undefined); setFieldErrors({});
    try {
      // 点击表达提交意图；Workbench 基于当前值执行一次整表校验，不受失焦校验的瞬时状态阻断。
      const validationOutcome = await validateFormIntent(definition, { value, selected: stableSelected, context }, validation, controller.signal);
      if (validationOutcome.kind === "cancelled" || validationOutcome.kind === "pending") return;
      if (validationOutcome.kind === "field-errors") { setFieldErrors(localizeFormFieldErrors(validationOutcome.fieldErrors, i18n.text)); return; }
      if (definition.workflow.confirmBeforeSubmit !== undefined && !await ui.confirm({ title: i18n.text(definition.workflow.title), content: i18n.text(definition.workflow.confirmBeforeSubmit) })) return;
      if (controller.signal.aborted) return;
      const outcome = await submitValidatedFormDefinition(definition, { value, selected: stableSelected, context }, controller.signal);
      if (outcome.kind === "cancelled") return;
      if (outcome.kind === "field-errors") { setFieldErrors(localizeFormFieldErrors(outcome.fieldErrors, i18n.text)); return; }
      const submittedValue = outcome.context.value;
      const sanitized = discardSecretMaterial(submittedValue, secretPointers);
      setValue(sanitized); setBaseline(JSON.stringify(sanitized));
      try {
        await runAfterSubmit(definition, outcome, controller.signal);
      } catch (error) {
        if (!controller.signal.aborted) setFailure(i18n.text(message(namespace, "form.afterSubmitFailed", "数据已提交，但提交后的处理失败：{reason}", { reason: errorText(error) })));
        return;
      }
      if (controller.signal.aborted) return;
      if (definition.workflow.success?.notify !== undefined) ui.notify({ title: i18n.text(definition.workflow.success.notify), kind: "success" });
      if (definition.workflow.success?.refreshCollection === true) onRefresh();
      if (resolveFormWorkflowSurface(definition.workflow) === "dialog" && definition.workflow.success?.close !== false) onClose?.();
    } catch (error) {
      if (!controller.signal.aborted) setFailure(errorText(error));
    } finally {
      setValue((current) => discardSecretMaterial(current, secretPointers));
      if (!controller.signal.aborted) setSubmitting(false);
    }
  }, [context, definition, i18n.text, loading, onClose, onRefresh, runningActionID, secretPointers, stableSelected, submitting, ui, validation, value]);

  return {
    definition, presentation, controlSize: presentation.size ?? definition?.size ?? definition?.workflow.size ?? formControlSize(presentation), schema, context, value, loading, submitting,
    ...(runningActionID === undefined ? {} : { runningActionID }), ...(failure === undefined ? {} : { failure }), fieldErrors, validation, ...(activeSection === undefined ? {} : { activeSection }),
    ...(validate === undefined ? {} : { validate }), change, setValidation, setActiveSection, requestClose, runAction, submit,
  };
}

function errorText(value: unknown): string { return value instanceof Error ? value.message : String(value); }
