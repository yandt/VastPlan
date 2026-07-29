import { usePortalI18n, usePortalUI } from "@vastplan/ui-primitives";
import { FormActions } from "./FormActions.js";
import { FormContent } from "./FormContent.js";
import type { FormWorkflowController } from "./useFormWorkflow.js";

export function FormPagePanel({ form }: { form: FormWorkflowController }) {
  const ui = usePortalUI();
  const i18n = usePortalI18n();
  const definition = form.definition;
  if (definition === undefined) return null;
  return <ui.Panel title={i18n.text(definition.workflow.title)}>
    <ui.Stack gap="md"><FormContent form={form} /><FormActions form={form} /></ui.Stack>
  </ui.Panel>;
}
