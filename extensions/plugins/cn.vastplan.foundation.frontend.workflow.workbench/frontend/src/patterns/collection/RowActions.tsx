import { useState } from "react";
import type { ActionSpec } from "@vastplan/ui-contract";
import { message, usePortalI18n, usePortalUI, type ComponentSize } from "@vastplan/ui-primitives";
import { evaluateFormCondition } from "../form/presentation.js";
import type { CollectionRow } from "./model.js";

const namespace = "cn.vastplan.foundation.frontend.workflow.workbench";
const directActionLimit = 2;

/**
 * The collection owns row-action layout. Functional plugins only declare actions
 * with placement=record.row and never construct framework-specific button groups.
 */
export function RowActions({ actions, row, size, onRunAction }: {
  actions: readonly ActionSpec[];
  row: CollectionRow;
  size: ComponentSize;
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
  return <ui.Stack direction="row" gap="sm" align="center" justify="center">
    {direct.map((action) => <ui.IconButton key={action.id} icon={action.icon} label={i18n.text(action.label)} size={size} tone={action.tone === "danger" ? "danger" : "normal"} onClick={() => onRunAction(action)} />)}
    {overflow.length === 0 ? null : <ui.Popover
      open={overflowOpen}
      placement="bottom-end"
      ariaLabel={moreLabel}
      onOpenChange={setOverflowOpen}
      trigger={(props) => <span ref={props.ref} aria-expanded={props["aria-expanded"]} aria-controls={props["aria-controls"]} onClick={props.onClick} onKeyDown={props.onKeyDown} style={{ display: "inline-flex", lineHeight: 0 }}><ui.IconButton icon="more" label={moreLabel} size={size} /></span>}
    ><ui.Menu size={size} variant="action" items={overflow.map((action) => ({ id: action.id, label: i18n.text(action.label), icon: <ui.Icon name={action.icon} />, disabled: false }))} onSelect={(id) => {
      const action = overflow.find((candidate) => candidate.id === id);
      if (action !== undefined) onRunAction(action);
      setOverflowOpen(false);
    }} /></ui.Popover>}
  </ui.Stack>;
}
