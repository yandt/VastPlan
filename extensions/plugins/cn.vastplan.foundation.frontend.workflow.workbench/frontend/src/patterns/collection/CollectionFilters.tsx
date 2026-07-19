import type { FilterSpec } from "@vastplan/ui-contract";
import { message, usePortalI18n, usePortalUI } from "@vastplan/ui-primitives";
import { collectionFilterSchema } from "./filter-schema.js";

const namespace = "cn.vastplan.foundation.frontend.workflow.workbench";

export function CollectionFilters({ filters, value, refreshing, onChange, onClear, onRefresh }: {
  filters: readonly FilterSpec[];
  value: Record<string, unknown>;
  refreshing: boolean;
  onChange(value: Record<string, unknown>): void;
  onClear(): void;
  onRefresh(): void;
}) {
  const ui = usePortalUI();
  const i18n = usePortalI18n();
  return <ui.FilterBar actions={<ui.Stack direction="row" gap="sm"><ui.Button kind="text" onClick={onClear}>{i18n.text(message(namespace, "action.clearFilters", "清除筛选"))}</ui.Button><ui.Button kind="secondary" onClick={onRefresh} loading={refreshing}>{i18n.text(message(namespace, "action.refresh", "刷新"))}</ui.Button></ui.Stack>}>
    <ui.FormRenderer schema={collectionFilterSchema(filters)} value={value} onChange={onChange} />
  </ui.FilterBar>;
}
