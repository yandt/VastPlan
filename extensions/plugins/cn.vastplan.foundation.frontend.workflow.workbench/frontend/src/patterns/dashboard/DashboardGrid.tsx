import type { CSSProperties, RefObject } from "react";
import { Responsive, horizontalCompactor, noCompactor, useContainerWidth, verticalCompactor } from "react-grid-layout";
import gridLayoutCSS from "react-grid-layout/css/styles.css";
import { dashboardDefaultBreakpoints, dashboardDefaultColumns } from "@vastplan/ui-contract";
import type { DashboardGridRuntimeProps } from "@vastplan/ui-primitives";
import { message, usePortalI18n, usePortalUI } from "@vastplan/ui-primitives";
import { fromReactGridLayouts, toReactGridLayouts } from "./layout-model.js";

const namespace = "cn.vastplan.foundation.frontend.workflow.workbench";

/** Deferred dashboard surface. The trusted host resolves semantic card IDs to content. */
export function DashboardGrid({ spec, cards, layouts, editable = false, onLayoutChange }: DashboardGridRuntimeProps) {
  const ui = usePortalUI();
  const i18n = usePortalI18n();
  const { width, containerRef, mounted } = useContainerWidth({ initialWidth: 1280 });
  const breakpoints = { ...dashboardDefaultBreakpoints, ...spec.breakpoints };
  const columns = { ...dashboardDefaultColumns, ...spec.columns };
  const gap = [spec.gap?.horizontal ?? 12, spec.gap?.vertical ?? 12] as const;
  const compactor = spec.compaction === "horizontal" ? horizontalCompactor : spec.compaction === "none" ? noCompactor : verticalCompactor;
  const rootStyle = {
    width: "100%", minWidth: 0,
    "--vastplan-dashboard-primary": ui.theme.tokens.color.primary,
    "--vastplan-dashboard-selected": ui.theme.tokens.color.selected,
  } as CSSProperties;

  return <div ref={containerRef as RefObject<HTMLDivElement>} style={rootStyle} data-vastplan-dashboard-grid={spec.id}>
    <style>{`${gridLayoutCSS}\n.react-grid-item.react-grid-placeholder{background:var(--vastplan-dashboard-primary);opacity:.16}.react-grid-item>.react-resizable-handle::after{border-color:var(--vastplan-dashboard-primary)}`}</style>
    {mounted ? <Responsive
      width={width}
      layouts={toReactGridLayouts(spec, layouts)}
      breakpoints={breakpoints}
      cols={columns}
      rowHeight={spec.rowHeight ?? 96}
      margin={gap}
      containerPadding={[0, 0]}
      compactor={compactor}
      dragConfig={{ enabled: editable, handle: "[data-vastplan-dashboard-drag-handle]" }}
      resizeConfig={{ enabled: editable, handles: ["se"] }}
      onLayoutChange={(_layout, nextLayouts) => onLayoutChange?.(fromReactGridLayouts(nextLayouts))}
    >
      {spec.cards.map((cardID) => <div key={cardID} data-vastplan-dashboard-card={cardID} style={{ minWidth: 0, minHeight: 0 }}>
        {editable ? <button type="button" data-vastplan-dashboard-drag-handle aria-label={`${i18n.text(message(namespace, "dashboard.move", "拖动卡片"))}: ${cardID}`} style={{ position: "absolute", zIndex: 2, top: 4, right: 4, width: 28, height: 28, padding: 0, border: 0, borderRadius: ui.theme.tokens.radius.sm, display: "grid", placeItems: "center", color: ui.theme.tokens.color.mutedText, background: ui.theme.tokens.color.overlaySurface, cursor: "grab" }}><ui.Icon name="drag" size="sm" /></button> : null}
        {cards[cardID] ?? <ui.ErrorState title={i18n.text(message(namespace, "dashboard.cardMissing", "卡片内容不可用"))} />}
      </div>)}
    </Responsive> : <ui.Skeleton rows={3} />}
  </div>;
}
