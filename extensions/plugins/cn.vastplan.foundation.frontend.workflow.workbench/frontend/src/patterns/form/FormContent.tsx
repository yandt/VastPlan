import { componentSizeRecipes, message, usePortalI18n, usePortalUI, type ComponentSize } from "@vastplan/ui-primitives";
import type { FormWorkflowController } from "./useFormWorkflow.js";

const namespace = "cn.vastplan.foundation.frontend.workflow.workbench";

export function FormContent({ form, showDescription = true, density = "default" }: { form: FormWorkflowController; showDescription?: boolean; density?: "default" | "dialog" }) {
  const ui = usePortalUI();
  const i18n = usePortalI18n();
  const controlSize = density === "dialog" ? formDialogControlSize(form.controlSize) : form.controlSize;
  const gap = density === "dialog" ? dialogContentGap(form.controlSize) : form.presentation.preset === "compact" ? "sm" : "md";
  return <ui.Stack gap={gap}>
    {!showDescription || form.definition?.workflow.description === undefined ? null : <p style={{ margin: 0 }}>{i18n.text(form.definition.workflow.description)}</p>}
    {form.failure === undefined ? null : <ui.ErrorState title={form.failure} />}
    {form.validation.errors.$form === undefined ? null : <ui.ErrorState title={form.validation.errors.$form} />}
    {form.loading || form.schema === undefined ? <ui.Skeleton rows={5} /> : <ui.FormRenderer
      schema={form.schema}
      value={form.value}
      onChange={form.change}
      size={controlSize}
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

/** FormDialog compacts only its field controls; the Dialog frame and action buttons retain their requested size. */
export function formDialogControlSize(size: ComponentSize): ComponentSize {
  return ({ xs: "xs", sm: "xs", md: "sm", lg: "md" } as const)[size];
}

function dialogContentGap(size: FormWorkflowController["controlSize"]): "xs" | "sm" | "md" {
  const gap = componentSizeRecipes.formDialog[size].contentGap;
  return gap <= 4 ? "xs" : gap <= 8 ? "sm" : "md";
}
