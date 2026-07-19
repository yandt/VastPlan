import type { ActionSpec, CollectionDensity, CollectionSpec } from "@vastplan/ui-contract";
import { message, usePortalI18n, usePortalUI } from "@vastplan/ui-primitives";
import type { CollectionColumnPreference, CollectionRow } from "./model.js";

const namespace = "cn.vastplan.foundation.frontend.workflow.workbench";

export function CollectionTable({ collection, columns, rows, selectedKeys, loading, density, keyOf, onSelectionChange, onRunAction }: {
  collection: CollectionSpec;
  columns: readonly CollectionColumnPreference[];
  rows: readonly CollectionRow[];
  selectedKeys: readonly string[];
  loading: boolean;
  density: CollectionDensity;
  keyOf(row: CollectionRow): string;
  onSelectionChange(keys: readonly string[]): void;
  onRunAction(action: ActionSpec, rows: readonly CollectionRow[]): void;
}) {
  const ui = usePortalUI();
  const i18n = usePortalI18n();
  const visibleColumns = columns.filter((column) => column.visible).map((column) => collection.columns.find((candidate) => candidate.key === column.key)).filter((column): column is NonNullable<typeof column> => column !== undefined);
  const rowActions = (collection.actions ?? []).filter((action) => action.placement === "record.row");
  const tableColumns = [
    ...visibleColumns.map((column) => ({ key: column.key, title: i18n.text(column.label), width: column.minWidth })),
    ...(rowActions.length === 0 ? [] : [{ key: "__actions", title: i18n.text(message(namespace, "column.actions", "操作")), render: (_value: unknown, row: CollectionRow) => <ui.Stack direction="row" gap="xs" wrap>{rowActions.map((action) => <ui.Button key={action.id} kind={action.tone ?? "text"} onClick={() => onRunAction(action, [row])}>{i18n.text(action.label)}</ui.Button>)}</ui.Stack> }]),
  ];
  return <ui.Table columns={tableColumns} rows={rows} rowKey={keyOf} selection={collection.selection ?? "none"} selectedRowKeys={selectedKeys} onSelectionChange={onSelectionChange} loading={loading} density={density} empty={<ui.EmptyState title={i18n.text(message(namespace, "empty.title", "暂无数据"))} />} />;
}
