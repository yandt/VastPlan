import { message, usePortalI18n, usePortalUI } from "@vastplan/ui-primitives";
import type { FormWorkflowController } from "./useFormWorkflow.js";

const namespace = "cn.vastplan.foundation.frontend.workflow.workbench";

export function FormContent({ form }: { form: FormWorkflowController }) {
  const ui = usePortalUI();
  const i18n = usePortalI18n();
  return <ui.Stack gap={form.presentation.preset === "compact" ? "sm" : "md"}>
    {form.definition?.workflow.description === undefined ? null : <p>{i18n.text(form.definition.workflow.description)}</p>}
    {form.failure === undefined ? null : <ui.ErrorState title={form.failure} />}
    {form.validation.errors.$form === undefined ? null : <ui.ErrorState title={form.validation.errors.$form} />}
    {form.loading || form.schema === undefined ? <ui.Skeleton rows={5} /> : <ui.FormRenderer
      schema={form.schema}
      value={form.value}
      onChange={form.change}
      size={form.controlSize}
      presentation={form.presentation}
      presentationSection={form.activeSection}
      onPresentationSectionChange={form.setActiveSection}
      submitting={form.submitting}
      errors={form.fieldErrors}
      context={form.context}
      validate={form.validate}
      onValidationChange={form.setValidation}
    />}
    {form.validation.validating ? <ui.Status tone="info">{i18n.text(message(namespace, "form.validating", "正在校验表单…"))}</ui.Status> : null}
  </ui.Stack>;
}
