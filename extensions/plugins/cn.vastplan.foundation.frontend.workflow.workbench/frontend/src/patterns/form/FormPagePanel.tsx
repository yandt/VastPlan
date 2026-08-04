import { message, usePortalI18n, usePortalUI } from "@vastplan/ui-primitives";
import { FormActions } from "./FormActions.js";
import { FormContent } from "./FormContent.js";
import type { FormWorkflowController } from "./useFormWorkflow.js";
import { ActionExecutionFeedback } from "../action/ActionExecutionFeedback.js";

const namespace = "cn.vastplan.foundation.frontend.workflow.workbench";

export function FormPagePanel({ form }: { form: FormWorkflowController }) {
  const ui = usePortalUI();
  const i18n = usePortalI18n();
  const definition = form.definition;
  if (definition === undefined) return null;
  return <><ui.BodySections sections={[{
    id: definition.id,
    title: i18n.text(definition.workflow.title),
    description: definition.workflow.description === undefined ? undefined : i18n.text(definition.workflow.description),
    content: <ui.Stack gap="md"><FormContent form={form} showDescription={false} /><FormActions form={form} /></ui.Stack>,
  }]} /><ActionExecutionFeedback active={form.submitting || form.runningActionID !== undefined} actionLabel={i18n.text(form.runningActionID === undefined ? definition.workflow.submitLabel ?? message(namespace, "action.submit", "提交") : definition.workflow.actions?.find((action) => action.id === form.runningActionID)?.label ?? message(namespace, "action.execute", "执行"))} /></>;
}
