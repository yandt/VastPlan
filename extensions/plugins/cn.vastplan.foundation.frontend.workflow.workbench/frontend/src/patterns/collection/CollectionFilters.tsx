import { useEffect, useState } from "react";
import type { FilterSpec } from "@vastplan/ui-contract";
import { message, usePortalI18n, usePortalUI } from "@vastplan/ui-primitives";
import { collectionFilterSchema } from "./filter-schema.js";

const namespace = "cn.vastplan.foundation.frontend.workflow.workbench";

const automaticFilterKinds = new Set<FilterSpec["kind"]>(["select", "boolean", "numberRange", "dateRange"]);

/** Three desktop fields occupy one query row; short filter bars apply without a separate query button. */
export function shouldAutoApplyCollectionFilters(filters: readonly FilterSpec[]): boolean { return filters.length <= 3; }

export function CollectionFilters({ filters, value, querying, onApply }: {
  filters: readonly FilterSpec[];
  value: Record<string, unknown>;
  querying: boolean;
  onApply(value: Record<string, unknown>): void;
}) {
  const ui = usePortalUI();
  const i18n = usePortalI18n();
  const autoApply = shouldAutoApplyCollectionFilters(filters);
  const [draft, setDraft] = useState(value);
  useEffect(() => setDraft(value), [value]);
  const clear = () => { setDraft({}); onApply({}); };
  const update = (filter: FilterSpec, patch: Record<string, unknown>) => {
    const next = { ...draft, ...patch };
    setDraft(next);
    if (autoApply && automaticFilterKinds.has(filter.kind)) onApply(next);
  };
  const actions = <ui.Stack direction="column" gap="sm" justify="between">
    <ui.Button kind="primary" onClick={() => onApply(draft)} loading={querying}>{i18n.text(message(namespace, "action.query", "查询"))}</ui.Button>
    <ui.Button kind="secondary" onClick={clear}>{i18n.text(message(namespace, "action.clearFilters", "重置"))}</ui.Button>
  </ui.Stack>;
  return <ui.FilterBar appearance="collection" actions={autoApply ? undefined : actions}>
    <ui.Grid columns={{ xs: 1, md: 2, xl: 3 }} gap="sm">{filters.map((filter) => <ui.GridItem key={filter.id}>
      <div onKeyDown={(event) => { if (autoApply && filter.kind === "text" && event.key === "Enter") { event.preventDefault(); onApply(draft); } }}>
        <ui.FormRenderer schema={collectionFilterSchema([filter])} value={{ [filter.id]: draft[filter.id] }} presentation={{ layout: "horizontal" }} onChange={(patch) => update(filter, patch)} />
      </div>
    </ui.GridItem>)}{autoApply ? <ui.GridItem><div style={{ display: "flex", justifyContent: "flex-end", alignItems: "center", minHeight: 32 }}><ui.Button kind="secondary" onClick={clear}>{i18n.text(message(namespace, "action.clearFilters", "重置"))}</ui.Button></div></ui.GridItem> : null}</ui.Grid>
  </ui.FilterBar>;
}
