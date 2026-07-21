import { useEffect, useState } from "react";
import { usePortalI18n, usePortalUI } from "@vastplan/ui-primitives";
import type { WorkbenchOverlayContent, WorkbenchOverlayDefinition } from "@vastplan/workbench-sdk";
import type { CollectionRow } from "../collection/model.js";
import { CollectionValue } from "../collection/CollectionValue.js";

export function CollectionOverlayWorkflow({ definition, selected, open, onClose }: { definition?: WorkbenchOverlayDefinition; selected: readonly CollectionRow[]; open: boolean; onClose(): void }) {
  const ui = usePortalUI();
  const i18n = usePortalI18n();
  const [content, setContent] = useState<WorkbenchOverlayContent>();
  const [failure, setFailure] = useState<string>();
  useEffect(() => {
    if (!open || definition === undefined) { setContent(undefined); setFailure(undefined); return; }
    const controller = new AbortController();
    setContent(undefined); setFailure(undefined);
    void definition.load(selected, controller.signal).then((value) => { if (!controller.signal.aborted) setContent(value); }).catch((error: unknown) => { if (!controller.signal.aborted) setFailure(error instanceof Error ? error.message : String(error)); });
    return () => controller.abort();
  }, [definition, open, selected]);
  if (definition === undefined) return null;
  const body = failure !== undefined ? <ui.ErrorState title={failure} /> : content === undefined ? <ui.Skeleton rows={6} /> : content.kind === "json"
    ? <ui.Grid columns={{ xs: 1, lg: Math.min(2, Math.max(1, content.documents.length)) }} gap="md">{content.documents.map((document, index) => <ui.GridItem key={index}><ui.Panel title={document.title === undefined ? undefined : i18n.text(document.title)}><pre style={{ overflow: "auto", maxHeight: 560, whiteSpace: "pre-wrap", overflowWrap: "anywhere" }}>{JSON.stringify(document.value, null, 2)}</pre></ui.Panel></ui.GridItem>)}</ui.Grid>
    : <ui.Table appearance="collection" rowKey={content.rowKey ?? "id"} rows={content.rows} columns={content.columns.map((column) => ({ key: column.key, title: i18n.text(column.label), width: column.minWidth, render: (value: unknown) => <CollectionValue column={column} value={value} /> }))} />;
  return definition.surface === "drawer" ? <ui.Drawer open={open} title={i18n.text(definition.title)} width={definition.size} onClose={onClose}>{body}</ui.Drawer> : <ui.Dialog open={open} title={i18n.text(definition.title)} width={definition.size} onClose={onClose}>{body}</ui.Dialog>;
}
