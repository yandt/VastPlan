import { useState } from "react";
import type { CollectionDensity, CollectionSpec } from "@vastplan/ui-contract";
import { message, usePortalI18n, usePortalUI } from "@vastplan/ui-primitives";
import type { CollectionColumnPreference } from "./model.js";
import { moveItem } from "./preferences.js";

const namespace = "cn.vastplan.foundation.frontend.workflow.workbench";

/** 列偏好属于即时生效的轻量选择，不需要阻断页面的 Dialog 草稿。 */
export function CollectionPreferencesPopover({ collection, columns, density, densityOptions, onChange }: {
  collection: CollectionSpec;
  columns: readonly CollectionColumnPreference[];
  density: CollectionDensity;
  densityOptions: readonly CollectionDensity[];
  onChange(columns: readonly CollectionColumnPreference[], density: CollectionDensity): void;
}) {
  const ui = usePortalUI();
  const i18n = usePortalI18n();
  const [open, setOpen] = useState(false);
  const title = i18n.text(message(namespace, "columns.title", "列设置"));
  const densityLabel = i18n.text(message(namespace, "density.title", "显示密度"));
  const updateColumns = (nextColumns: readonly CollectionColumnPreference[]) => onChange(nextColumns, density);
  return <ui.Popover open={open} placement="bottom-end" ariaLabel={title} initialFocus="first" onOpenChange={(next) => setOpen(next)} trigger={(trigger) => <button
    ref={(node) => trigger.ref(node)} type="button" title={title} aria-label={title} aria-expanded={trigger["aria-expanded"]} aria-controls={trigger["aria-controls"]}
    onClick={trigger.onClick} onKeyDown={trigger.onKeyDown}
    style={{ width: 36, height: 36, display: "inline-grid", placeItems: "center", border: 0, borderRadius: 6, background: "transparent", color: "inherit", cursor: "pointer" }}
  ><ui.Icon name="columns" /></button>}>
    <ui.Stack gap="sm"><div style={{ minWidth: 260 }}>
      {densityOptions.length <= 1 ? null : <label><ui.Stack gap="xs"><span>{densityLabel}</span><ui.Select ariaLabel={densityLabel} value={density} options={densityOptions.map((value) => ({ value, label: i18n.text(message(namespace, `density.${value}`, densityFallback(value))) }))} onChange={(value) => {
        if (value === "compact" || value === "standard" || value === "comfortable") onChange(columns, value);
      }} /></ui.Stack></label>}
      <ui.Stack gap="xs">{columns.map((column, index) => <ui.Stack key={column.key} direction="row" gap="sm" align="center">
        <ui.Button kind="text" onClick={() => updateColumns(moveItem(columns, index, -1))} disabled={index === 0}>↑</ui.Button>
        <ui.Button kind="text" onClick={() => updateColumns(moveItem(columns, index, 1))} disabled={index === columns.length - 1}>↓</ui.Button>
        <ui.Button kind={column.visible ? "secondary" : "text"} onClick={() => updateColumns(columns.map((item) => item.key === column.key ? { ...item, visible: !item.visible } : item))}>{column.visible ? i18n.text(message(namespace, "action.hide", "隐藏")) : i18n.text(message(namespace, "action.show", "显示"))}</ui.Button>
        <span>{i18n.text(collection.columns.find((item) => item.key === column.key)?.label ?? message(namespace, "column.unknown", "未知列"))}</span>
      </ui.Stack>)}</ui.Stack>
    </div></ui.Stack>
  </ui.Popover>;
}

function densityFallback(value: CollectionDensity): string {
  return value === "compact" ? "紧凑" : value === "comfortable" ? "宽松" : "标准";
}
