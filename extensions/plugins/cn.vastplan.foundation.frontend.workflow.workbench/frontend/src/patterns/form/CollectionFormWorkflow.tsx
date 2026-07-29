import type { WorkbenchFormDefinition } from "@vastplan/workbench-sdk";
import type { CollectionRow } from "../collection/model.js";
import { FormActions } from "./FormActions.js";
import { FormContent } from "./FormContent.js";
import { FormSurface } from "./FormSurface.js";
import { useFormWorkflow } from "./useFormWorkflow.js";

export function CollectionFormWorkflow({ definition, selected, open, onClose, onRefresh, onDirtyChange }: {
  definition?: WorkbenchFormDefinition;
  selected: readonly CollectionRow[];
  open: boolean;
  onClose?(): void;
  onRefresh(): void;
  onDirtyChange?(dirty: boolean): void;
}) {
  const form = useFormWorkflow({ definition, selected, open, onClose, onRefresh, onDirtyChange });
  if (definition === undefined) return null;
  const actions = <FormActions form={form} />;
  return <FormSurface definition={definition} open={open} submitting={form.submitting} actions={actions} onClose={form.requestClose}>
    <FormContent form={form} />
  </FormSurface>;
}
