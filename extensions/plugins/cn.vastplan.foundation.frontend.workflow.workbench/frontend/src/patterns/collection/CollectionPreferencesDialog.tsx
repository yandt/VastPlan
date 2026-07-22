import { useEffect, useState } from "react";
import type { CollectionDensity, CollectionSpec } from "@vastplan/ui-contract";
import { message, usePortalI18n, usePortalUI } from "@vastplan/ui-primitives";
import type { CollectionColumnPreference } from "./model.js";
import { moveItem } from "./preferences.js";

const namespace = "cn.vastplan.foundation.frontend.workflow.workbench";

export function CollectionPreferencesDialog({ open, collection, columns, density, densityOptions, onApply, onClose }: {
  open: boolean;
  collection: CollectionSpec;
  columns: readonly CollectionColumnPreference[];
  density: CollectionDensity;
  densityOptions: readonly CollectionDensity[];
  onApply(columns: readonly CollectionColumnPreference[], density: CollectionDensity): void;
  onClose(): void;
}) {
  const ui = usePortalUI();
  const i18n = usePortalI18n();
  const [draftColumns, setDraftColumns] = useState(columns);
  const [draftDensity, setDraftDensity] = useState(density);
  useEffect(() => { if (open) { setDraftColumns(columns); setDraftDensity(density); } }, [columns, density, open]);
  const densityLabel = i18n.text(message(namespace, "density.title", "显示密度"));
  return <ui.Dialog open={open} title={i18n.text(message(namespace, "columns.title", "列设置"))} onClose={onClose} footer={<ui.Button kind="primary" onClick={() => { onApply(draftColumns, draftDensity); onClose(); }}>{i18n.text(message(namespace, "action.done", "完成"))}</ui.Button>}>
    <ui.Stack gap="sm">
      {densityOptions.length <= 1 ? null : <label><ui.Stack gap="xs"><span>{densityLabel}</span><ui.Select ariaLabel={densityLabel} value={draftDensity} options={densityOptions.map((value) => ({ value, label: i18n.text(message(namespace, `density.${value}`, densityFallback(value))) }))} onChange={(value) => { if (value === "compact" || value === "standard" || value === "comfortable") setDraftDensity(value); }} /></ui.Stack></label>}
      {draftColumns.map((column, index) => <ui.Stack key={column.key} direction="row" gap="sm" align="center">
      <ui.Button kind="text" onClick={() => setDraftColumns(moveItem(draftColumns, index, -1))} disabled={index === 0}>↑</ui.Button>
      <ui.Button kind="text" onClick={() => setDraftColumns(moveItem(draftColumns, index, 1))} disabled={index === draftColumns.length - 1}>↓</ui.Button>
      <ui.Button kind={column.visible ? "secondary" : "text"} onClick={() => setDraftColumns(draftColumns.map((item) => item.key === column.key ? { ...item, visible: !item.visible } : item))}>{column.visible ? i18n.text(message(namespace, "action.hide", "隐藏")) : i18n.text(message(namespace, "action.show", "显示"))}</ui.Button>
      <span>{i18n.text(collection.columns.find((item) => item.key === column.key)?.label ?? message(namespace, "column.unknown", "未知列"))}</span>
    </ui.Stack>)}</ui.Stack>
  </ui.Dialog>;
}

function densityFallback(value: CollectionDensity): string {
  return value === "compact" ? "紧凑" : value === "comfortable" ? "宽松" : "标准";
}
