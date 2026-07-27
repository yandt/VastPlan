import type { ActionSpec } from "@vastplan/ui-contract";
import { message, usePortalI18n, usePortalUI } from "@vastplan/ui-primitives";
import { useEffect, useState } from "react";
import type { ReactNode } from "react";

const namespace = "cn.vastplan.foundation.frontend.workflow.workbench";

export function CollectionToolbar({ hasFilters, refreshing, selectedCount, toolbarActions, bulkActions, onRefresh, preferences, onRunAction }: {
  hasFilters: boolean;
  refreshing: boolean;
  selectedCount: number;
  toolbarActions: readonly ActionSpec[];
  bulkActions: readonly ActionSpec[];
  onRefresh(): void;
  preferences?: ReactNode;
  onRunAction(action: ActionSpec): void;
}) {
  const ui = usePortalUI();
  const i18n = usePortalI18n();
  const [bulkActionID, setBulkActionID] = useState<string>();
  useEffect(() => {
    if (bulkActionID !== undefined && !bulkActions.some((action) => action.id === bulkActionID)) setBulkActionID(undefined);
  }, [bulkActionID, bulkActions]);
  const selectedBulkAction = bulkActions.find((action) => action.id === bulkActionID);
  const gap = ui.theme.tokens.spacing.sm;
  return <div style={{ width: "100%", minWidth: 0, display: "flex", alignItems: "center", flexWrap: "wrap", gap }}>
      <div style={{ minWidth: 0, flex: "1 1 auto", display: "flex", alignItems: "center", flexWrap: "wrap", gap }}>
        {bulkActions.length === 0 ? null : <>
          <span>{i18n.text(message(namespace, "selection.count", "已选择 {count} 项", { count: selectedCount }))}</span>
          <ui.Select ariaLabel={i18n.text(message(namespace, "bulk.select", "选择批量操作"))} placeholder={i18n.text(message(namespace, "bulk.placeholder", "选择批量操作"))} value={bulkActionID} disabled={selectedCount === 0} options={bulkActions.map((action) => ({ value: action.id, label: i18n.text(action.label) }))} onChange={setBulkActionID} />
          <ui.IconButton icon={selectedBulkAction?.icon ?? "success"} label={i18n.text(message(namespace, "bulk.execute", "执行"))} disabled={selectedCount === 0 || selectedBulkAction === undefined} tone={selectedBulkAction?.tone === "danger" ? "danger" : selectedBulkAction?.tone === "primary" ? "primary" : "normal"} onClick={() => selectedBulkAction === undefined ? undefined : onRunAction(selectedBulkAction)} />
        </>}
        {toolbarActions.map((action) => <ui.IconButton key={action.id} icon={action.icon} label={i18n.text(action.label)} disabled={Boolean(action.requiresSelection && selectedCount === 0)} tone={action.tone === "danger" ? "danger" : action.tone === "primary" ? "primary" : "normal"} onClick={() => onRunAction(action)} />)}
      </div>
      <div style={{ flex: "0 0 auto", marginLeft: "auto", display: "flex", alignItems: "center", gap }}>
        {hasFilters ? null : <ui.IconButton icon="refresh" label={i18n.text(message(namespace, "action.refresh", "刷新"))} onClick={onRefresh} loading={refreshing} />}
        {preferences}
      </div>
    </div>;
}
