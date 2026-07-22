import type { ActionSpec } from "@vastplan/ui-contract";
import { message, usePortalI18n, usePortalUI } from "@vastplan/ui-primitives";
import { useEffect, useState } from "react";

const namespace = "cn.vastplan.foundation.frontend.workflow.workbench";

export function CollectionToolbar({ hasFilters, refreshing, selectedCount, toolbarActions, bulkActions, onRefresh, onColumns, onRunAction }: {
  hasFilters: boolean;
  refreshing: boolean;
  selectedCount: number;
  toolbarActions: readonly ActionSpec[];
  bulkActions: readonly ActionSpec[];
  onRefresh(): void;
  onColumns?(): void;
  onRunAction(action: ActionSpec): void;
}) {
  const ui = usePortalUI();
  const i18n = usePortalI18n();
  const [bulkActionID, setBulkActionID] = useState<string>();
  useEffect(() => {
    if (bulkActionID !== undefined && !bulkActions.some((action) => action.id === bulkActionID)) setBulkActionID(undefined);
  }, [bulkActionID, bulkActions]);
  const selectedBulkAction = bulkActions.find((action) => action.id === bulkActionID);
  return <>
    <ui.Stack direction="row" gap="sm" wrap justify="between">
      <ui.Stack direction="row" gap="sm" wrap>
        {bulkActions.length === 0 ? null : <>
          <span>{i18n.text(message(namespace, "selection.count", "已选择 {count} 项", { count: selectedCount }))}</span>
          <ui.Select ariaLabel={i18n.text(message(namespace, "bulk.select", "选择批量操作"))} placeholder={i18n.text(message(namespace, "bulk.placeholder", "选择批量操作"))} value={bulkActionID} disabled={selectedCount === 0} options={bulkActions.map((action) => ({ value: action.id, label: i18n.text(action.label) }))} onChange={setBulkActionID} />
          <ui.Button kind={selectedBulkAction?.tone ?? "secondary"} disabled={selectedCount === 0 || selectedBulkAction === undefined} onClick={() => selectedBulkAction === undefined ? undefined : onRunAction(selectedBulkAction)}>{i18n.text(message(namespace, "bulk.execute", "执行"))}</ui.Button>
        </>}
        {toolbarActions.map((action) => <ui.Button key={action.id} kind={action.tone ?? "primary"} disabled={Boolean(action.requiresSelection && selectedCount === 0)} onClick={() => onRunAction(action)}>{i18n.text(action.label)}</ui.Button>)}
      </ui.Stack>
      <ui.Stack direction="row" gap="sm" wrap>
        {hasFilters ? null : <ui.IconButton icon="refresh" label={i18n.text(message(namespace, "action.refresh", "刷新"))} onClick={onRefresh} loading={refreshing} />}
        {onColumns === undefined ? null : <ui.IconButton icon="columns" label={i18n.text(message(namespace, "action.columns", "列设置"))} onClick={onColumns} />}
      </ui.Stack>
    </ui.Stack>
  </>;
}
