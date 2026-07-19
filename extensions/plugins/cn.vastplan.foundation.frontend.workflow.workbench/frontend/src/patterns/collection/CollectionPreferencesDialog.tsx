import type { CollectionSpec } from "@vastplan/ui-contract";
import { message, usePortalI18n, usePortalUI } from "@vastplan/ui-primitives";
import type { CollectionColumnPreference } from "./model.js";
import { moveItem } from "./preferences.js";

const namespace = "cn.vastplan.foundation.frontend.workflow.workbench";

export function CollectionPreferencesDialog({ open, collection, columns, onChange, onClose }: {
  open: boolean;
  collection: CollectionSpec;
  columns: readonly CollectionColumnPreference[];
  onChange(columns: CollectionColumnPreference[]): void;
  onClose(): void;
}) {
  const ui = usePortalUI();
  const i18n = usePortalI18n();
  return <ui.Dialog open={open} title={i18n.text(message(namespace, "columns.title", "列设置"))} onClose={onClose} footer={<ui.Button kind="primary" onClick={onClose}>{i18n.text(message(namespace, "action.done", "完成"))}</ui.Button>}>
    <ui.Stack gap="sm">{columns.map((column, index) => <ui.Stack key={column.key} direction="row" gap="sm" align="center">
      <ui.Button kind="text" onClick={() => onChange(moveItem(columns, index, -1))} disabled={index === 0}>↑</ui.Button>
      <ui.Button kind="text" onClick={() => onChange(moveItem(columns, index, 1))} disabled={index === columns.length - 1}>↓</ui.Button>
      <ui.Button kind={column.visible ? "secondary" : "text"} onClick={() => onChange(columns.map((item) => item.key === column.key ? { ...item, visible: !item.visible } : item))}>{column.visible ? i18n.text(message(namespace, "action.hide", "隐藏")) : i18n.text(message(namespace, "action.show", "显示"))}</ui.Button>
      <span>{i18n.text(collection.columns.find((item) => item.key === column.key)?.label ?? message(namespace, "column.unknown", "未知列"))}</span>
    </ui.Stack>)}</ui.Stack>
  </ui.Dialog>;
}
