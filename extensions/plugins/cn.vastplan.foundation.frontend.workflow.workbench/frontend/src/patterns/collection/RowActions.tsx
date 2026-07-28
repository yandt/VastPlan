import { useState } from "react";
import type { ActionSpec } from "@vastplan/ui-contract";
import { message, usePortalI18n, usePortalUI } from "@vastplan/ui-primitives";
import { evaluateFormCondition } from "../form/presentation.js";
import type { CollectionRow } from "./model.js";

const namespace = "cn.vastplan.foundation.frontend.workflow.workbench";
const directActionLimit = 2;

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
  const [overflowOpen, setOverflowOpen] = useState(false);
  const visible = actions.filter((action) => action.visibleWhen === undefined || evaluateFormCondition(action.visibleWhen, row));
  if (visible.length === 0) return null;
  const direct = visible.slice(0, directActionLimit);
  const overflow = visible.slice(direct.length);
  const moreLabel = i18n.text(message(namespace, "action.moreRow", "更多行操作"));
  return <ui.Stack direction="row" gap="xs" align="center" justify="center">
    {direct.map((action) => <ui.IconButton key={action.id} icon={action.icon} label={i18n.text(action.label)} size="compact" tone={action.tone === "danger" ? "danger" : action.tone === "primary" ? "primary" : "normal"} onClick={() => onRunAction(action)} />)}
    {overflow.length === 0 ? null : <ui.Popover
      open={overflowOpen}
      placement="bottom-end"
      ariaLabel={moreLabel}
      onOpenChange={setOverflowOpen}
      trigger={(props) => <span ref={props.ref} aria-expanded={props["aria-expanded"]} aria-controls={props["aria-controls"]} onClick={props.onClick} onKeyDown={props.onKeyDown}><ui.IconButton icon="more" label={moreLabel} size="compact" /></span>}
    ><ui.Stack direction="row" gap="xs" align="center" justify="center" wrap>{overflow.map((action) => <ui.IconButton key={action.id} icon={action.icon} label={i18n.text(action.label)} size="compact" tone={action.tone === "danger" ? "danger" : action.tone === "primary" ? "primary" : "normal"} onClick={() => { onRunAction(action); setOverflowOpen(false); }} />)}</ui.Stack></ui.Popover>}
  </ui.Stack>;
}
