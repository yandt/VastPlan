import { resolveFormWorkflowSurface, type WorkbenchFormDefinition } from "@vastplan/workbench-sdk";
import type { CollectionRow } from "../collection/model.js";
import { FormDialog } from "./FormDialog.js";
import { FormPagePanel } from "./FormPagePanel.js";
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
  return resolveFormWorkflowSurface(definition.workflow) === "page"
    ? <FormPagePanel form={form} />
    : <FormDialog form={form} open={open} />;
}
