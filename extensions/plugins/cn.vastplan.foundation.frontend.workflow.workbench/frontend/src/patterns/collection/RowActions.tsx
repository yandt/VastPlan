import type { ActionSpec } from "@vastplan/ui-contract";
import { message, usePortalI18n, usePortalUI } from "@vastplan/ui-primitives";
import { ActionMenuPopover } from "../action/ActionMenuPopover.js";
import { evaluateFormCondition } from "../form/presentation.js";
import type { CollectionRow } from "./model.js";

const namespace = "cn.vastplan.foundation.frontend.workflow.workbench";
const directActionLimit = 2;
const rowActionSize = "sm" as const;

/**
 * The collection owns row-action layout. Functional plugins only declare actions
 * with placement=record.row and never construct framework-specific button groups.
 */
export function RowActions({ actions, row, onRunAction }: {
  actions: readonly ActionSpec[];
  row: CollectionRow;
  onRunAction(action: ActionSpec): void;
}) {
  const ui = usePortalUI();
  const i18n = usePortalI18n();
  const visible = actions.filter((action) => action.visibleWhen === undefined || evaluateFormCondition(action.visibleWhen, row));
  if (visible.length === 0) return null;
  const direct = visible.slice(0, directActionLimit);
  const overflow = visible.slice(direct.length);
  const moreLabel = i18n.text(message(namespace, "action.moreRow", "更多行操作"));
  return <ui.Stack direction="row" gap="sm" align="center" justify="center">
    {direct.map((action) => <ui.IconButton key={action.id} icon={action.icon} label={i18n.text(action.label)} size={rowActionSize} tone={action.tone === "danger" ? "danger" : "normal"} onClick={() => onRunAction(action)} />)}
    <ActionMenuPopover label={moreLabel} items={overflow.map((action) => ({
      id: action.id,
      label: i18n.text(action.label),
      icon: action.icon,
      tone: action.tone === "danger" ? "danger" : "normal",
    }))} triggerSize={rowActionSize} menuSize={rowActionSize} iconSize={rowActionSize} onSelect={(id) => {
      const action = overflow.find((candidate) => candidate.id === id);
      if (action !== undefined) onRunAction(action);
    }} />
  </ui.Stack>;
}
