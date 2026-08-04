import { message, usePortalI18n, usePortalUI } from "@vastplan/ui-primitives";
import type { FormWorkflowController } from "./useFormWorkflow.js";

const namespace = "cn.vastplan.foundation.frontend.workflow.workbench";

export function FormActions({ form }: { form: FormWorkflowController }) {
  const ui = usePortalUI();
  const i18n = usePortalI18n();
  const sections = form.presentation.sections ?? [];
  const sectionIndex = Math.max(0, sections.findIndex((section) => section.id === form.activeSection));
  const steps = form.presentation.navigation === "steps" && sections.length > 0;
  const actions = form.definition?.workflow.actions ?? [];
  const running = form.submitting || form.runningActionID !== undefined;
  const renderActions = (placement: "footer.start" | "footer.end") => actions.filter((action) => action.placement === placement).map((action) =>
    <ui.Button key={action.id} kind={action.tone === "danger" ? "danger" : action.tone === "primary" ? "primary" : "secondary"} loading={form.runningActionID === action.id} disabled={running || form.loading || action.requiresValid !== false && (!form.validation.valid || form.validation.validating)} onClick={() => void form.runAction(action)}>
      <ui.Stack direction="row" gap="xs" align="center"><ui.Icon name={action.icon} size="xs" />{i18n.text(action.label)}</ui.Stack>
    </ui.Button>);
  return <ui.Stack direction="row" gap="sm" justify="between" align="center" wrap>
    <ui.Stack direction="row" gap="sm" align="center" wrap fill={false}>{renderActions("footer.start")}</ui.Stack>
    <ui.Stack direction="row" gap="sm" align="center" justify="end" wrap fill={false}>
    {renderActions("footer.end")}
    <ui.Button kind="secondary" disabled={running} onClick={() => void form.requestClose()}>{i18n.text(form.definition?.workflow.cancelLabel ?? message(namespace, "action.cancel", "取消"))}</ui.Button>
    {steps && sectionIndex > 0 ? <ui.Button kind="secondary" disabled={running} onClick={() => form.setActiveSection(sections[sectionIndex - 1]?.id)}>{i18n.text(message(namespace, "action.previous", "上一步"))}</ui.Button> : null}
    {steps && sectionIndex < sections.length - 1
      ? <ui.Button kind="primary" disabled={running} onClick={() => form.setActiveSection(sections[sectionIndex + 1]?.id)}>{i18n.text(message(namespace, "action.next", "下一步"))}</ui.Button>
      : <ui.Button kind="primary" loading={form.submitting} disabled={running || form.validation.validating || form.loading} onClick={() => void form.submit()}>{i18n.text(form.definition?.workflow.submitLabel ?? message(namespace, "action.submit", "提交"))}</ui.Button>}
    </ui.Stack>
  </ui.Stack>;
}
