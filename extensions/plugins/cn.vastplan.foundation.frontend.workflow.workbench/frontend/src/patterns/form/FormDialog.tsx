import { usePortalI18n, usePortalUI } from "@vastplan/ui-primitives";
import { FormActions } from "./FormActions.js";
import { FormContent } from "./FormContent.js";
import type { FormWorkflowController } from "./useFormWorkflow.js";

/** The default governed composition for every non-page form workflow. The adapter keeps only this body scrollable. */
export function FormDialog({ form, open }: { form: FormWorkflowController; open: boolean }) {
  const ui = usePortalUI();
  const i18n = usePortalI18n();
  const definition = form.definition;
  if (definition === undefined) return null;
  return <ui.Dialog
    open={open}
    title={i18n.text(definition.workflow.title)}
    description={definition.workflow.description === undefined ? undefined : i18n.text(definition.workflow.description)}
    size={definition.workflow.size ?? definition.size}
    variant="form"
    width={definition.workflow.dialogWidth}
    height={definition.workflow.dialogHeight}
    contentOverflow="scroll"
    footer={<FormActions form={form} />}
    onClose={() => { if (!form.submitting) void form.requestClose(); }}
  >
    <FormContent form={form} density="dialog" showDescription={false} />
  </ui.Dialog>;
}
