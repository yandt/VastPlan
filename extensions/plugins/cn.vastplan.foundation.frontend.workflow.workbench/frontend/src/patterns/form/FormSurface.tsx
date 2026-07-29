import type { ReactNode } from "react";
import { usePortalI18n, usePortalUI } from "@vastplan/ui-primitives";
import type { WorkbenchFormDefinition } from "@vastplan/workbench-sdk";

export function FormSurface({ definition, open, submitting, actions, children, onClose }: {
  definition: WorkbenchFormDefinition;
  open: boolean;
  submitting: boolean;
  actions: ReactNode;
  children: ReactNode;
  onClose(): Promise<void>;
}) {
  const ui = usePortalUI();
  const i18n = usePortalI18n();
  const title = i18n.text(definition.workflow.title);
  if (definition.workflow.surface === "page") {
    return <ui.Panel title={title}><ui.Stack gap="md">{children}{actions}</ui.Stack></ui.Panel>;
  }
  const props = { open, title, width: definition.workflow.size, footer: actions, onClose: () => { if (!submitting) void onClose(); } } as const;
  return definition.workflow.surface === "drawer"
    ? <ui.Drawer {...props}>{children}</ui.Drawer>
    : <ui.Dialog {...props}>{children}</ui.Dialog>;
}
