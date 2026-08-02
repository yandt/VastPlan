import { useCallback, useMemo, useState, type ReactNode } from "react";
import type { ActionSpec } from "@vastplan/ui-contract";
import { usePortalI18n, usePortalUI } from "@vastplan/ui-primitives";
import type { RecordActionContext, RecordPageDefinition } from "@vastplan/workbench-sdk";
import { evaluateFormCondition } from "../form/presentation.js";
import { CollectionFormWorkflow } from "../form/CollectionFormWorkflow.js";
import { CollectionOverlayWorkflow } from "../overlay/CollectionOverlayWorkflow.js";
import { RecordPane } from "./RecordPane.js";
import type { RecordLoaderState } from "./useRecordLoader.js";
import { WorkbenchPageFlow } from "../layout/WorkbenchRhythm.js";
import { useActionExecution } from "../action/ActionExecutionFeedback.js";

export function RecordWorkspace({ page, data, refresh, primary, primaryLabel, splitMode = "both", onBack, onDirtyChange }: {
  page: RecordPageDefinition;
  data: RecordLoaderState;
  refresh(): void;
  primary?: ReactNode;
  primaryLabel?: string;
  splitMode?: "both" | "primary" | "secondary";
  onBack?(): void;
  onDirtyChange(dirty: boolean): void;
}) {
  const ui = usePortalUI();
  const i18n = usePortalI18n();
  const [activeForm, setActiveForm] = useState<string>();
  const [activeOverlay, setActiveOverlay] = useState<string>();
  const execution = useActionExecution();
  const record = data.record;
  const selected = useMemo(() => record === undefined ? [] : [record], [record]);
  const visible = useCallback((action: ActionSpec) => action.visibleWhen === undefined || record !== undefined && evaluateFormCondition(action.visibleWhen, record), [record]);
  const actions = page.actions ?? [];
  const detailActions = actions.filter((action) => action.placement === "record.detail" && visible(action));
  const runAction = useCallback(async (action: ActionSpec) => {
    if (execution.running) return;
    if (action.requiresSelection && record === undefined) return;
    if (action.form !== undefined) { setActiveForm(action.form); return; }
    if (action.overlay !== undefined) { setActiveOverlay(action.overlay); return; }
    const title = i18n.text(action.label);
    if (action.confirm !== undefined && !await ui.confirm({ title, content: i18n.text(action.confirm) })) return;
    try {
      const context: RecordActionContext = { action, record, refresh };
      const outcome = await execution.run({ id: action.id, label: title }, (signal) => page.runAction?.(context, signal) ?? Promise.resolve(undefined));
      if (!outcome.completed) return;
      const result = outcome.value;
      if (result?.notify !== undefined) ui.notify({ title: i18n.text(result.notify.title), content: result.notify.content === undefined ? undefined : i18n.text(result.notify.content), kind: result.notify.kind });
      refresh();
    } catch (error) {
      ui.notify({ title, content: error instanceof Error ? error.message : String(error), kind: "error" });
    }
  }, [execution, i18n, page, record, refresh, ui]);
  const secondary = <RecordPane detail={page.detail} record={record} editor={page.editor} actions={detailActions} loading={data.loading} failure={data.failure} runningActionID={execution.active?.id} actionRunning={execution.running} onRetry={refresh} onAction={(action) => void runAction(action)} onDirtyChange={onDirtyChange} onBack={onBack} />;
  return <>
    <WorkbenchPageFlow size={page.size}>{primary === undefined ? secondary : <ui.SplitView primaryLabel={primaryLabel ?? "Records"} secondaryLabel={i18n.text(page.title)} primary={primary} secondary={secondary} mode={splitMode} primaryWidth="md" />}</WorkbenchPageFlow>
    <CollectionFormWorkflow definition={page.forms?.find((form) => form.id === activeForm)} selected={selected} open={activeForm !== undefined} onClose={() => setActiveForm(undefined)} onRefresh={refresh} />
    <CollectionOverlayWorkflow definition={page.overlays?.find((overlay) => overlay.id === activeOverlay)} selected={selected} open={activeOverlay !== undefined} onClose={() => setActiveOverlay(undefined)} />
    {execution.feedback}
  </>;
}
