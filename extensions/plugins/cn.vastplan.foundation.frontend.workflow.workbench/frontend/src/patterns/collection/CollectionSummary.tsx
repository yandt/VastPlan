import { usePortalI18n, usePortalUI } from "@vastplan/ui-primitives";
import type { CollectionSummary as CollectionSummaryDefinition } from "@vastplan/workbench-sdk";

const defaultColumns = { xs: 1, sm: 1, md: 2, lg: 2, xl: 3 } as const;

/** Projects a semantic collection summary without exposing renderer-specific nodes to plugins. */
export function CollectionSummary({ summary }: { summary: CollectionSummaryDefinition }) {
  const ui = usePortalUI();
  const i18n = usePortalI18n();
  const title = summary.title === undefined ? undefined : i18n.text(summary.title);
  const descriptions = <ui.Descriptions
    size={summary.size}
    title={summary.appearance === "plain" ? title : undefined}
    columns={summary.columns ?? defaultColumns}
    items={summary.metrics.map((metric) => ({
      id: metric.id,
      label: i18n.text(metric.label),
      value: metric.tone === undefined ? metric.value : <ui.Status tone={metric.tone}>{metric.value}</ui.Status>,
      span: metric.span,
    }))}
  />;

  return <div style={{ width: "100%", minWidth: 0 }}>
    {summary.appearance === "plain" ? descriptions : <ui.Panel size={summary.size} title={title}>{descriptions}</ui.Panel>}
  </div>;
}
