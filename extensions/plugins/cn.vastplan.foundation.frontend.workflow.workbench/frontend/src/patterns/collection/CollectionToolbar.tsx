import type { ActionSpec } from "@vastplan/ui-contract";
import { message, usePortalI18n, usePortalUI } from "@vastplan/ui-primitives";

const namespace = "cn.vastplan.foundation.frontend.workflow.workbench";

export function CollectionToolbar({ hasFilters, refreshing, selectedCount, primaryActions, secondaryActions, bulkActions, onRefresh, onColumns, onRunAction }: {
  hasFilters: boolean;
  refreshing: boolean;
  selectedCount: number;
  primaryActions: readonly ActionSpec[];
  secondaryActions: readonly ActionSpec[];
  bulkActions: readonly ActionSpec[];
  onRefresh(): void;
  onColumns(): void;
  onRunAction(action: ActionSpec): void;
}) {
  const ui = usePortalUI();
  const i18n = usePortalI18n();
  return <>
    <ui.Stack direction="row" gap="sm" wrap justify="between">
      <ui.Stack direction="row" gap="sm" wrap>{primaryActions.map((action) => <ui.Button key={action.id} kind={action.tone ?? "primary"} disabled={Boolean(action.requiresSelection && selectedCount === 0)} onClick={() => onRunAction(action)}>{i18n.text(action.label)}</ui.Button>)}</ui.Stack>
      <ui.Stack direction="row" gap="sm" wrap>
        {hasFilters ? null : <ui.Button kind="secondary" onClick={onRefresh} loading={refreshing}>{i18n.text(message(namespace, "action.refresh", "刷新"))}</ui.Button>}
        <ui.Button kind="secondary" onClick={onColumns}>{i18n.text(message(namespace, "action.columns", "列设置"))}</ui.Button>
        {secondaryActions.map((action) => <ui.Button key={action.id} kind={action.tone ?? "secondary"} disabled={Boolean(action.requiresSelection && selectedCount === 0)} onClick={() => onRunAction(action)}>{i18n.text(action.label)}</ui.Button>)}
      </ui.Stack>
    </ui.Stack>
    {bulkActions.length === 0 ? null : <ui.Stack direction="row" gap="sm" wrap><span>{i18n.text(message(namespace, "selection.count", "已选择 {count} 项", { count: selectedCount }))}</span>{bulkActions.map((action) => <ui.Button key={action.id} kind={action.tone ?? "secondary"} disabled={selectedCount === 0} onClick={() => onRunAction(action)}>{i18n.text(action.label)}</ui.Button>)}</ui.Stack>}
  </>;
}
