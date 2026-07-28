import { useEffect, useMemo, useRef, useState } from "react";
import type { PageActionSpec } from "@vastplan/ui-contract";
import { message, usePortalI18n, usePortalUI } from "@vastplan/ui-primitives";
import type { PageActionHostDefinition } from "@vastplan/workbench-sdk";
import { ActionMenuPopover } from "./ActionMenuPopover.js";
import { CollectionFormWorkflow } from "../form/CollectionFormWorkflow.js";
import { CollectionOverlayWorkflow } from "../overlay/CollectionOverlayWorkflow.js";

const namespace = "cn.vastplan.foundation.frontend.workflow.workbench";
const directActionLimit = 4;
const emptySelection: readonly Record<string, unknown>[] = Object.freeze([]);

export function PageActionHost({ definition, onRefresh }: { definition: PageActionHostDefinition; onRefresh(): void }) {
  const ui = usePortalUI();
  const i18n = usePortalI18n();
  const [activeForm, setActiveForm] = useState<string>();
  const [activeOverlay, setActiveOverlay] = useState<string>();
  const [running, setRunning] = useState<string>();
  const runRef = useRef<AbortController>();
  const layout = useMemo(() => pageActionLayout(definition.actions), [definition.actions]);
  useEffect(() => () => runRef.current?.abort(), []);

  const run = async (action: PageActionSpec) => {
    if (running !== undefined) return;
    if (action.form !== undefined) { setActiveForm(action.form); return; }
    if (action.overlay !== undefined) { setActiveOverlay(action.overlay); return; }
    const title = i18n.text(action.label);
    if (action.confirm !== undefined && !await ui.confirm({ title, content: i18n.text(action.confirm) })) return;
    runRef.current?.abort();
    const controller = new AbortController();
    runRef.current = controller;
    setRunning(action.id);
    try {
      const result = await definition.runAction?.({ action, refresh: onRefresh }, controller.signal);
      if (controller.signal.aborted) return;
      if (result?.notify !== undefined) ui.notify({ title: i18n.text(result.notify.title), content: result.notify.content === undefined ? undefined : i18n.text(result.notify.content), kind: result.notify.kind });
      onRefresh();
    } catch (error) {
      if (!controller.signal.aborted) ui.notify({ title, content: error instanceof Error ? error.message : String(error), kind: "error" });
    } finally {
      if (!controller.signal.aborted) setRunning(undefined);
    }
  };

  return <>
    <div data-vastplan-page-actions style={{ width: "100%", minWidth: 0, display: "flex", justifyContent: "flex-end", alignItems: "center", gap: 4 }}>
      {layout.direct.map((action) => <PageActionButton key={action.id} action={action} loading={running === action.id} disabled={running !== undefined && running !== action.id} onRun={() => void run(action)} />)}
      <ActionMenuPopover
        label={i18n.text(message(namespace, "action.more", "更多页面操作"))}
        triggerSize="lg"
        items={layout.overflow.map((action) => ({ id: action.id, label: i18n.text(action.label), icon: action.icon, disabled: running !== undefined, tone: action.tone === "danger" ? "danger" : "normal" }))}
        onSelect={(id) => {
        const action = layout.overflow.find((candidate) => candidate.id === id);
        if (action !== undefined) void run(action);
      }} />
    </div>
    <CollectionFormWorkflow definition={definition.forms?.find((form) => form.id === activeForm)} selected={emptySelection} open={activeForm !== undefined} onClose={() => setActiveForm(undefined)} onRefresh={onRefresh} />
    <CollectionOverlayWorkflow definition={definition.overlays?.find((overlay) => overlay.id === activeOverlay)} selected={emptySelection} open={activeOverlay !== undefined} onClose={() => setActiveOverlay(undefined)} />
  </>;
}

function PageActionButton({ action, loading, disabled, onRun }: { action: PageActionSpec; loading: boolean; disabled: boolean; onRun(): void }) {
  const ui = usePortalUI();
  const i18n = usePortalI18n();
  const label = i18n.text(action.label);
  const display = action.display ?? "icon";
  if (display === "icon") return <ui.IconButton size="lg" icon={action.icon} label={label} loading={loading} disabled={disabled} tone={action.tone === "danger" ? "danger" : action.tone === "primary" ? "primary" : "normal"} onClick={onRun} />;
  return <ui.Button size="lg" kind={action.tone === "danger" ? "danger" : action.tone === "primary" ? "primary" : "secondary"} loading={loading} disabled={disabled} onClick={onRun}>
    {display === "label" ? null : <ui.Icon name={action.icon} />} {label}
  </ui.Button>;
}

export function pageActionLayout(actions: readonly PageActionSpec[]): { direct: readonly PageActionSpec[]; overflow: readonly PageActionSpec[] } {
  const ordered = [...actions].sort((left, right) => (left.order ?? 0) - (right.order ?? 0) || left.id.localeCompare(right.id));
  const direct = ordered.filter((action) => action.overflow === "never");
  const automatic = ordered.filter((action) => (action.overflow ?? "auto") === "auto");
  direct.push(...automatic.splice(0, Math.max(0, directActionLimit - direct.length)));
  const directIDs = new Set(direct.map((action) => action.id));
  return Object.freeze({ direct: Object.freeze(direct), overflow: Object.freeze(ordered.filter((action) => !directIDs.has(action.id))) });
}
