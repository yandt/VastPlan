import { ComponentSizeProvider, componentSizeRecipes, useComponentSize, usePortalI18n, usePortalUI } from "@vastplan/ui-primitives";
import type { CollectionSummary as CollectionSummaryDefinition } from "@vastplan/workbench-sdk";

const defaultColumns = { xs: 1, sm: 1, md: 2, lg: 2, xl: 3 } as const;

/** Projects a semantic collection summary without exposing renderer-specific nodes to plugins. */
export function CollectionSummary({ summary }: { summary: CollectionSummaryDefinition }) {
  const ui = usePortalUI();
  const i18n = usePortalI18n();
  const size = useComponentSize(summary.size);
  const title = summary.title === undefined ? undefined : i18n.text(summary.title);
  const descriptions = <ui.Descriptions
    size={size}
    columns={summary.columns ?? defaultColumns}
    items={summary.metrics.map((metric) => ({
      id: metric.id,
      label: i18n.text(metric.label),
      value: metric.tone === undefined ? metric.value : <ui.Status tone={metric.tone}>{metric.value}</ui.Status>,
      span: metric.span,
    }))}
  />;

  return <div style={{ width: "100%", minWidth: 0 }}>
    {summary.appearance === "plain" ? <ComponentSizeProvider size={size}><div
      data-collection-summary="strip"
      data-size={size}
      role="group"
      aria-label={title}
      style={{ display: "flex", flexWrap: "wrap", alignItems: "center", minHeight: componentSizeRecipes.control[size].height, columnGap: componentSizeRecipes.layout[size].sectionGap, rowGap: componentSizeRecipes.layout[size].gap }}
    >
      {title === undefined ? null : <strong style={{ fontSize: componentSizeRecipes.descriptions[size].fontSize }}>{title}</strong>}
      {summary.metrics.map((metric) => <div key={metric.id} style={{ display: "flex", alignItems: "center", gap: componentSizeRecipes.layout[size].gap, whiteSpace: "nowrap" }}>
        <span style={{ color: ui.theme.tokens.color.mutedText, fontSize: componentSizeRecipes.descriptions[size].fontSize }}>{i18n.text(metric.label)}</span>
        <span style={{ color: ui.theme.tokens.color.text, fontSize: componentSizeRecipes.descriptions[size].fontSize, fontWeight: 600 }}>{metric.tone === undefined ? metric.value : <ui.Status size={size} tone={metric.tone}>{metric.value}</ui.Status>}</span>
      </div>)}
    </div></ComponentSizeProvider> : <ui.Panel size={size} title={title}>{descriptions}</ui.Panel>}
  </div>;
}
