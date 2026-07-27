import type { DashboardBreakpoint, DashboardGridItem, DashboardGridLayouts, DashboardGridSpec } from "@vastplan/ui-contract";
import type { Layout, LayoutItem, ResponsiveLayouts } from "react-grid-layout";

export function toReactGridLayouts(spec: DashboardGridSpec, override?: DashboardGridLayouts): ResponsiveLayouts<DashboardBreakpoint> {
  const layouts = override ?? spec.layouts;
  return Object.fromEntries(Object.entries(layouts).map(([breakpoint, items]) => [breakpoint, items?.map(toReactGridItem) ?? []])) as ResponsiveLayouts<DashboardBreakpoint>;
}

export function fromReactGridLayouts(layouts: ResponsiveLayouts<DashboardBreakpoint>): DashboardGridLayouts {
  return Object.freeze(Object.fromEntries(Object.entries(layouts).map(([breakpoint, items]) => [breakpoint, Object.freeze((items ?? []).map(fromReactGridItem))])));
}

function toReactGridItem(item: DashboardGridItem): LayoutItem {
  return {
    i: item.cardID, x: item.x, y: item.y, w: item.width, h: item.height,
    ...(item.minWidth === undefined ? {} : { minW: item.minWidth }),
    ...(item.minHeight === undefined ? {} : { minH: item.minHeight }),
    ...(item.maxWidth === undefined ? {} : { maxW: item.maxWidth }),
    ...(item.maxHeight === undefined ? {} : { maxH: item.maxHeight }),
    ...(item.static === undefined ? {} : { static: item.static }),
  };
}

function fromReactGridItem(item: Layout[number]): DashboardGridItem {
  return Object.freeze({
    cardID: item.i, x: item.x, y: item.y, width: item.w, height: item.h,
    ...(item.minW === undefined ? {} : { minWidth: item.minW }),
    ...(item.minH === undefined ? {} : { minHeight: item.minH }),
    ...(item.maxW === undefined ? {} : { maxWidth: item.maxW }),
    ...(item.maxH === undefined ? {} : { maxHeight: item.maxH }),
    ...(item.static === undefined ? {} : { static: item.static }),
  });
}
