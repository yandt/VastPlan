import { useMemo, useState, useSyncExternalStore } from "react";
import type { ActionSpec } from "@vastplan/ui-contract";
import { message, usePortalI18n, usePortalUI } from "@vastplan/ui-primitives";
import type { CollectionPageDefinition } from "@vastplan/workbench-sdk";
import { pageActionController } from "../action/page-action-controller.js";

const namespace = "cn.vastplan.foundation.frontend.workflow.workbench";
const directActionLimit = 4;

/** Page-level actions are mounted by the trusted host into page.header.end. */
export function CollectionPageActions({ page }: { page: CollectionPageDefinition }) {
  const ui = usePortalUI();
  const i18n = usePortalI18n();
  const controller = useMemo(() => pageActionController(page), [page]);
  const snapshot = useSyncExternalStore(controller.subscribe, controller.getSnapshot, controller.getSnapshot);
  const [overflowOpen, setOverflowOpen] = useState(false);
  const actions = (page.collection.actions ?? []).filter((action) =>
    (action.placement === "page.primary" || action.placement === "page.secondary") && snapshot.visibleActionIDs.has(action.id));
  if (actions.length === 0) return null;
  const direct = actions.length <= directActionLimit ? actions : actions.slice(0, directActionLimit - 1);
  const overflow = actions.slice(direct.length);
  const disabled = (action: ActionSpec) => !snapshot.ready || Boolean(action.requiresSelection && snapshot.selectedCount === 0);
  const run = (action: ActionSpec) => controller.run(action);
  return <ui.Stack direction="row" gap="xs" align="center">
    {direct.map((action) => <ui.IconButton key={action.id} icon={action.icon ?? "more"} label={i18n.text(action.label)} disabled={disabled(action)} tone={action.tone === "danger" ? "danger" : action.tone === "primary" ? "primary" : "normal"} onClick={() => run(action)} />)}
    {overflow.length === 0 ? null : <ui.Popover
      open={overflowOpen}
      placement="bottom-end"
      ariaLabel={i18n.text(message(namespace, "action.more", "更多页面操作"))}
      onOpenChange={setOverflowOpen}
      trigger={(props) => <span ref={props.ref} aria-expanded={props["aria-expanded"]} aria-controls={props["aria-controls"]} onClick={props.onClick} onKeyDown={props.onKeyDown}><ui.IconButton icon="more" label={i18n.text(message(namespace, "action.more", "更多页面操作"))} /></span>}
    ><ui.Menu items={overflow.map((action) => ({ id: action.id, label: i18n.text(action.label), icon: <ui.Icon name={action.icon ?? "more"} />, disabled: disabled(action) }))} onSelect={(id) => {
      const action = overflow.find((candidate) => candidate.id === id);
      if (action !== undefined) run(action);
      setOverflowOpen(false);
    }} /></ui.Popover>}
  </ui.Stack>;
}
