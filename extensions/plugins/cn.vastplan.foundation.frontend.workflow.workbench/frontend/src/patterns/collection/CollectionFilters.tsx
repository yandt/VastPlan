import { useEffect, useState } from "react";
import type { CollectionFilterLayout, FilterSpec, ResponsiveColumnCount } from "@vastplan/ui-contract";
import { message, usePortalI18n, usePortalUI } from "@vastplan/ui-primitives";
import { collectionFilterSchema } from "./filter-schema.js";

const namespace = "cn.vastplan.foundation.frontend.workflow.workbench";

const automaticFilterKinds = new Set<FilterSpec["kind"]>(["select", "boolean", "numberRange", "dateRange"]);
const responsiveBreakpoints = ["xs", "sm", "md", "lg", "xl"] as const;
const defaultFilterColumns = Object.freeze({ xs: 1, md: 2, xl: 4 });

/** 未显式配置时，筛选区在桌面最多四列。 */
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

/**
 * 多行筛选的操作单元从下一可用网格列开始，并跨到行尾；按钮在该单元内右对齐，
 * 因而每个响应式断点都会落在最后一行的最后一列。
 */
export function collectionFilterActionSpan(filters: readonly FilterSpec[], columns: ResponsiveColumnCount): ResponsiveColumnCount {
  const remaining = (count: number) => {
    const normalized = Math.max(1, Math.floor(count));
    const occupied = filters.length % normalized;
    return occupied === 0 ? normalized : normalized - occupied;
  };
  if (typeof columns === "number") return remaining(columns);

  let inherited = 1;
  const spans: Exclude<ResponsiveColumnCount, number> = {};
  for (const breakpoint of responsiveBreakpoints) {
    inherited = columns[breakpoint] ?? inherited;
    spans[breakpoint] = remaining(inherited);
  }
  return spans;
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
  return <ui.FilterBar appearance="collection">
    <ui.Grid columns={columns} gap="xs">{filters.map((filter) => <ui.GridItem key={filter.id}>
      <div onKeyDown={(event) => { if (autoApply && filter.kind === "text" && event.key === "Enter") { event.preventDefault(); onApply(draft); } }}>
        <ui.FormRenderer schema={collectionFilterSchema([filter])} value={{ [filter.id]: draft[filter.id] }} presentation={{ layout: "compact" }} onChange={(patch) => update(filter, patch)} />
      </div>
    </ui.GridItem>)}{autoApply ? null : <ui.GridItem span={collectionFilterActionSpan(filters, columns)}>
      <ui.Stack direction="column" gap="xs" align="end">
        <ui.Button kind="primary" onClick={() => onApply(draft)} loading={querying}>{i18n.text(message(namespace, "action.query", "查询"))}</ui.Button>
        <ui.Button kind="secondary" onClick={clear}>{i18n.text(message(namespace, "action.clearFilters", "清除"))}</ui.Button>
      </ui.Stack>
    </ui.GridItem>}</ui.Grid>
  </ui.FilterBar>;
}
