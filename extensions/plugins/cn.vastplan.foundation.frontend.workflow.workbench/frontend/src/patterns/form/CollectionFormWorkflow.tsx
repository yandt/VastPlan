import { useEffect, useMemo, useRef, useState } from "react";
import { message, usePortalI18n, usePortalUI, type FormRendererProps, type FormRendererValidationState } from "@vastplan/ui-primitives";
import type { WorkbenchFormDefinition } from "@vastplan/workbench-sdk";
import { projectFormPresentation } from "./presentation.js";
import type { CollectionRow } from "../collection/model.js";

const namespace = "cn.vastplan.foundation.frontend.workflow.workbench";
const emptyValidation: FormRendererValidationState = { valid: false, validating: false, issues: [], errors: {} };
const emptyContext: Readonly<Record<string, unknown>> = Object.freeze({});

export function CollectionFormWorkflow({ definition, selected, open, onClose, onRefresh }: {
  definition?: WorkbenchFormDefinition;
  selected: readonly CollectionRow[];
  open: boolean;
  onClose?(): void;
  onRefresh(): void;
}) {
  const ui = usePortalUI();
  const i18n = usePortalI18n();
  const [value, setValue] = useState<Record<string, unknown>>({});
  const [baseline, setBaseline] = useState("{}");
  const [loading, setLoading] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [failure, setFailure] = useState<string>();
  const [fieldErrors, setFieldErrors] = useState<Readonly<Record<string, string>>>({});
  const [validation, setValidation] = useState<FormRendererValidationState>(emptyValidation);
  const [activeSection, setActiveSection] = useState<string>();
  const loadRef = useRef<AbortController>();
  const submitRef = useRef<AbortController>();
  const context = definition?.context ?? emptyContext;
  useEffect(() => {
    if (!open || definition === undefined) return;
    loadRef.current?.abort();
    const controller = new AbortController();
    loadRef.current = controller;
    setLoading(true); setFailure(undefined); setFieldErrors({}); setValidation(emptyValidation);
    setActiveSection(definition.presentation?.sections?.[0]?.id);
    const source = definition.load?.(selected, controller.signal) ?? Promise.resolve(definition.initialValue ?? {});
    void source.then((loaded) => {
      if (controller.signal.aborted) return;
      const next = clone(loaded);
      setValue(next); setBaseline(JSON.stringify(next));
    }).catch((error: unknown) => { if (!controller.signal.aborted) setFailure(errorText(error)); })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [definition, open, selected]);
  useEffect(() => () => submitRef.current?.abort(), []);
  const schema = useMemo(() => definition === undefined ? undefined : projectFormPresentation(definition.schema, definition.presentation, value, context, i18n.text), [context, definition, i18n.text, value]);
  const asyncValidator = useMemo<FormRendererProps["validate"]>(() => definition?.validate === undefined ? undefined : async ({ value: next, context: nextContext, signal }) => {
    const errors = await definition.validate!({ value: next, context: nextContext, signal });
    return Object.fromEntries(Object.entries(errors).map(([pointer, error]) => [pointer, i18n.text(error)]));
  }, [definition, i18n.text]);
  if (definition === undefined) return null;
  const dirty = JSON.stringify(value) !== baseline;
  const sections = definition.presentation?.sections ?? [];
  const sectionIndex = Math.max(0, sections.findIndex((section) => section.id === activeSection));
  const steps = definition.presentation?.navigation === "steps" && sections.length > 0;
  const requestClose = async () => {
    if (submitting) return;
    if (dirty && !await ui.confirm({ title: i18n.text(message(namespace, "form.discardTitle", "放弃未保存的修改？")), content: i18n.text(message(namespace, "form.discardContent", "关闭后，本次输入不会保留。")) })) return;
    if (definition.workflow.surface === "page") {
      setValue(JSON.parse(baseline) as Record<string, unknown>); setFieldErrors({}); setFailure(undefined);
    } else onClose?.();
  };
  const submit = async () => {
    if (submitting || !validation.valid || validation.validating) return;
    if (definition.workflow.confirmBeforeSubmit !== undefined && !await ui.confirm({ title: i18n.text(definition.workflow.title), content: i18n.text(definition.workflow.confirmBeforeSubmit) })) return;
    submitRef.current?.abort();
    const controller = new AbortController();
    submitRef.current = controller;
    setSubmitting(true); setFailure(undefined); setFieldErrors({});
    try {
      const result = await definition.submit({ value, selected }, controller.signal);
      if (controller.signal.aborted) return;
      if (result?.fieldErrors !== undefined && Object.keys(result.fieldErrors).length > 0) {
        setFieldErrors(Object.fromEntries(Object.entries(result.fieldErrors).map(([pointer, error]) => [pointer, i18n.text(error)])));
        return;
      }
      if (definition.workflow.success?.notify !== undefined) ui.notify({ title: i18n.text(definition.workflow.success.notify), kind: "success" });
      if (definition.workflow.success?.refreshCollection === true) onRefresh();
      setBaseline(JSON.stringify(value));
      if (definition.workflow.surface !== "page" && definition.workflow.success?.close !== false) onClose?.();
    } catch (error) {
      if (!controller.signal.aborted) setFailure(errorText(error));
    } finally {
      if (!controller.signal.aborted) setSubmitting(false);
    }
  };
  const footer = <ui.Stack direction="row" gap="sm" justify="end" wrap>
    <ui.Button kind="secondary" disabled={submitting} onClick={() => void requestClose()}>{i18n.text(definition.workflow.cancelLabel ?? message(namespace, "action.cancel", "取消"))}</ui.Button>
    {steps && sectionIndex > 0 ? <ui.Button kind="secondary" disabled={submitting} onClick={() => setActiveSection(sections[sectionIndex - 1]?.id)}>{i18n.text(message(namespace, "action.previous", "上一步"))}</ui.Button> : null}
    {steps && sectionIndex < sections.length - 1 ? <ui.Button kind="primary" disabled={submitting} onClick={() => setActiveSection(sections[sectionIndex + 1]?.id)}>{i18n.text(message(namespace, "action.next", "下一步"))}</ui.Button>
      : <ui.Button kind="primary" loading={submitting} disabled={!validation.valid || validation.validating || loading} onClick={() => void submit()}>{i18n.text(definition.workflow.submitLabel ?? message(namespace, "action.submit", "提交"))}</ui.Button>}
  </ui.Stack>;
  const content = <ui.Stack gap="md">
    {definition.workflow.description === undefined ? null : <p>{i18n.text(definition.workflow.description)}</p>}
    {failure === undefined ? null : <ui.ErrorState title={failure} />}
    {loading || schema === undefined ? <ui.Skeleton rows={5} /> : <ui.FormRenderer
      schema={schema}
      value={value}
      onChange={(next) => { setValue(next); setFieldErrors({}); setFailure(undefined); }}
      presentation={definition.presentation}
      presentationSection={activeSection}
      onPresentationSectionChange={setActiveSection}
      submitting={submitting}
      errors={fieldErrors}
      context={context}
      validate={asyncValidator}
      onValidationChange={setValidation}
    />}
  </ui.Stack>;
  const title = i18n.text(definition.workflow.title);
  if (definition.workflow.surface === "page") return <ui.Panel title={title}>{content}<div style={{ marginTop: 16 }}>{footer}</div></ui.Panel>;
  return definition.workflow.surface === "drawer"
    ? <ui.Drawer open={open} title={title} width={definition.workflow.size} footer={footer} onClose={() => void requestClose()}>{content}</ui.Drawer>
    : <ui.Dialog open={open} title={title} width={definition.workflow.size} footer={footer} onClose={() => void requestClose()}>{content}</ui.Dialog>;
}

function clone(value: Readonly<Record<string, unknown>>): Record<string, unknown> { return JSON.parse(JSON.stringify(value)) as Record<string, unknown>; }
function errorText(value: unknown): string { return value instanceof Error ? value.message : String(value); }
