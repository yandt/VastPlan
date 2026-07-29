import { message, usePortalI18n, usePortalUI } from "@vastplan/ui-primitives";
import type { FormWorkflowController } from "./useFormWorkflow.js";

const namespace = "cn.vastplan.foundation.frontend.workflow.workbench";

export function FormActions({ form }: { form: FormWorkflowController }) {
  const ui = usePortalUI();
  const i18n = usePortalI18n();
  const sections = form.presentation.sections ?? [];
  const sectionIndex = Math.max(0, sections.findIndex((section) => section.id === form.activeSection));
  const steps = form.presentation.navigation === "steps" && sections.length > 0;
  return <ui.Stack direction="row" gap="sm" justify="end" wrap>
    <ui.Button kind="secondary" disabled={form.submitting} onClick={() => void form.requestClose()}>{i18n.text(form.definition?.workflow.cancelLabel ?? message(namespace, "action.cancel", "取消"))}</ui.Button>
    {steps && sectionIndex > 0 ? <ui.Button kind="secondary" disabled={form.submitting} onClick={() => form.setActiveSection(sections[sectionIndex - 1]?.id)}>{i18n.text(message(namespace, "action.previous", "上一步"))}</ui.Button> : null}
    {steps && sectionIndex < sections.length - 1
      ? <ui.Button kind="primary" disabled={form.submitting} onClick={() => form.setActiveSection(sections[sectionIndex + 1]?.id)}>{i18n.text(message(namespace, "action.next", "下一步"))}</ui.Button>
      : <ui.Button kind="primary" loading={form.submitting} disabled={!form.validation.valid || form.validation.validating || form.loading} onClick={() => void form.submit()}>{i18n.text(form.definition?.workflow.submitLabel ?? message(namespace, "action.submit", "提交"))}</ui.Button>}
  </ui.Stack>;
}
