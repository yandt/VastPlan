import { useState } from "react";
import type { CollectionDensity, CollectionSpec } from "@vastplan/ui-contract";
import { message, usePortalI18n, usePortalUI } from "@vastplan/ui-primitives";
import type { CollectionColumnPreference } from "./model.js";
import { CollectionDensityButtons } from "./CollectionDensityButtons.js";
import { ColumnPreferenceList } from "./ColumnPreferenceList.js";

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
  const densityLabels: Record<CollectionDensity, string> = {
    compact: i18n.text(message(namespace, "density.compact", "紧凑")),
    standard: i18n.text(message(namespace, "density.standard", "标准")),
    comfortable: i18n.text(message(namespace, "density.comfortable", "宽松")),
  };
  const columnLabels = Object.fromEntries(columns.map((column) => [column.key, i18n.text(collection.columns.find((item) => item.key === column.key)?.label ?? message(namespace, "column.unknown", "未知列"))]));
  return <ui.Popover open={open} placement="bottom-end" ariaLabel={title} initialFocus="first" onOpenChange={(next) => setOpen(next)} trigger={(trigger) => <button
    ref={(node) => trigger.ref(node)} type="button" title={title} aria-label={title} aria-expanded={trigger["aria-expanded"]} aria-controls={trigger["aria-controls"]}
    onClick={trigger.onClick} onKeyDown={trigger.onKeyDown}
    style={{ width: 36, height: 36, display: "inline-grid", placeItems: "center", border: 0, borderRadius: 6, background: "transparent", color: "inherit", cursor: "pointer" }}
  ><ui.Icon name="columns" /></button>}>
    <div style={{ boxSizing: "border-box", width: 292, display: "grid", gap: 7 }}>
      <CollectionDensityButtons label={densityLabel} value={density} options={densityOptions} labels={densityLabels} onChange={(next) => onChange(columns, next)} />
      {densityOptions.length <= 1 ? null : <div style={{ height: 1, background: ui.theme.tokens.color.border }} />}
      <ColumnPreferenceList columns={columns} labels={columnLabels} dragLabel={i18n.text(message(namespace, "columns.drag", "拖动调整顺序"))} showLabel={i18n.text(message(namespace, "action.show", "显示"))} hideLabel={i18n.text(message(namespace, "action.hide", "隐藏"))} onChange={updateColumns} />
    </div>
  </ui.Popover>;
}
