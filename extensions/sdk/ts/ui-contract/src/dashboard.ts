/** Governed responsive layout for future Workbench dashboard/home cards. */
export type DashboardBreakpoint = "xxs" | "xs" | "sm" | "md" | "lg";
export type DashboardCompaction = "vertical" | "horizontal" | "none";

export const dashboardBreakpointOrder = Object.freeze(["xxs", "xs", "sm", "md", "lg"] as const satisfies readonly DashboardBreakpoint[]);
export const dashboardDefaultBreakpoints: Readonly<Record<DashboardBreakpoint, number>> = Object.freeze({ xxs: 0, xs: 480, sm: 768, md: 996, lg: 1200 });
export const dashboardDefaultColumns: Readonly<Record<DashboardBreakpoint, number>> = Object.freeze({ xxs: 2, xs: 4, sm: 6, md: 10, lg: 12 });

export interface DashboardGridItem {
  cardID: string;
  x: number;
  y: number;
  width: number;
  height: number;
  minWidth?: number;
  minHeight?: number;
  maxWidth?: number;
  maxHeight?: number;
  static?: boolean;
}

export type DashboardGridLayouts = Readonly<Partial<Record<DashboardBreakpoint, readonly DashboardGridItem[]>>>;

export interface DashboardGridSpec {
  id: string;
  /** Stable semantic card IDs resolved by a trusted dashboard host. */
  cards: readonly string[];
  layouts: DashboardGridLayouts;
  columns?: Readonly<Partial<Record<DashboardBreakpoint, number>>>;
  breakpoints?: Readonly<Partial<Record<DashboardBreakpoint, number>>>;
  rowHeight?: number;
  gap?: Readonly<{ horizontal?: number; vertical?: number }>;
  compaction?: DashboardCompaction;
}
