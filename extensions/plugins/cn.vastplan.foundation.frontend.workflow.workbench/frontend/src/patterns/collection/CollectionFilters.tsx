import { useEffect, useState } from "react";
import type { CollectionFilterLayout, FilterSpec, ResponsiveColumnCount } from "@vastplan/ui-contract";
import { message, usePortalI18n, usePortalUI } from "@vastplan/ui-primitives";
import { collectionFilterSchema } from "./filter-schema.js";

const namespace = "cn.vastplan.foundation.frontend.workflow.workbench";

const automaticFilterKinds = new Set<FilterSpec["kind"]>(["select", "boolean", "numberRange", "dateRange"]);
const defaultFilterColumns = Object.freeze({ xs: 1, md: 2, xl: 3 });

/** 未显式配置时，筛选区在桌面最多三列。 */
export function collectionFilterColumns(layout: CollectionFilterLayout | undefined): ResponsiveColumnCount {
  return layout?.columns ?? defaultFilterColumns;
}

/** 单行筛选直接响应；超过当前桌面列数时使用查询草稿模式。 */
export function shouldAutoApplyCollectionFilters(filters: readonly FilterSpec[], columns: ResponsiveColumnCount = defaultFilterColumns): boolean {
  return filters.length <= largestColumnCount(columns);
}

function largestColumnCount(columns: ResponsiveColumnCount): number {
  if (typeof columns === "number") return columns;
  return Math.max(1, ...Object.values(columns).filter((value): value is number => typeof value === "number"));
}

export function CollectionFilters({ filters, layout, value, querying, onApply }: {
  filters: readonly FilterSpec[];
  layout?: CollectionFilterLayout;
  value: Record<string, unknown>;
  querying: boolean;
  onApply(value: Record<string, unknown>): void;
}) {
  const ui = usePortalUI();
  const i18n = usePortalI18n();
  const columns = collectionFilterColumns(layout);
  const autoApply = shouldAutoApplyCollectionFilters(filters, columns);
  const [draft, setDraft] = useState(value);
  useEffect(() => setDraft(value), [value]);
  const clear = () => { setDraft({}); onApply({}); };
  const update = (filter: FilterSpec, patch: Record<string, unknown>) => {
    const next = { ...draft, ...patch };
    setDraft(next);
    if (autoApply && automaticFilterKinds.has(filter.kind)) onApply(next);
  };
  const actions = <ui.Stack direction="column" gap="sm" justify="between">
    {autoApply ? null : <ui.Button kind="primary" onClick={() => onApply(draft)} loading={querying}>{i18n.text(message(namespace, "action.query", "查询"))}</ui.Button>}
    <ui.Button kind="secondary" onClick={clear}>{i18n.text(message(namespace, "action.clearFilters", "重置"))}</ui.Button>
  </ui.Stack>;
  return <ui.FilterBar appearance="collection" actions={actions}>
    <ui.Grid columns={columns} gap="sm">{filters.map((filter) => <ui.GridItem key={filter.id}>
      <div onKeyDown={(event) => { if (autoApply && filter.kind === "text" && event.key === "Enter") { event.preventDefault(); onApply(draft); } }}>
        <ui.FormRenderer schema={collectionFilterSchema([filter])} value={{ [filter.id]: draft[filter.id] }} presentation={{ layout: "horizontal" }} onChange={(patch) => update(filter, patch)} />
      </div>
    </ui.GridItem>)}</ui.Grid>
  </ui.FilterBar>;
}
