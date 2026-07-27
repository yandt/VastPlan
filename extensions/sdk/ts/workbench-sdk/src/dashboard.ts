import {
  dashboardBreakpointOrder,
  dashboardDefaultBreakpoints,
  dashboardDefaultColumns,
  type DashboardBreakpoint,
  type DashboardGridItem,
  type DashboardGridSpec,
} from "@vastplan/ui-contract";

/** Validates and freezes the JSON-only dashboard layout before future registration. */
export function defineDashboardGrid(spec: DashboardGridSpec): DashboardGridSpec {
  if (!validID(spec.id) || !Array.isArray(spec.cards) || spec.cards.length === 0 || spec.cards.length > 100 || new Set(spec.cards).size !== spec.cards.length || spec.cards.some((id) => !validID(id))) {
    throw new Error(`Dashboard Grid ${spec.id} 的卡片定义无效或重复`);
  }
  if (spec.compaction !== undefined && spec.compaction !== "vertical" && spec.compaction !== "horizontal" && spec.compaction !== "none") throw new Error("Dashboard compaction 无效");
  validateInteger(spec.rowHeight ?? 96, 24, 400, "Dashboard rowHeight");
  validateInteger(spec.gap?.horizontal ?? 12, 0, 48, "Dashboard horizontal gap");
  validateInteger(spec.gap?.vertical ?? 12, 0, 48, "Dashboard vertical gap");
  const columns = { ...dashboardDefaultColumns, ...spec.columns };
  const widths = { ...dashboardDefaultBreakpoints, ...spec.breakpoints };
  if (unknownBreakpoints(spec.columns) || unknownBreakpoints(spec.breakpoints)) throw new Error("Dashboard 包含未知断点");
  validateBreakpoints(columns, widths);
  const layouts = Object.entries(spec.layouts ?? {});
  if (layouts.length === 0 || layouts.some(([breakpoint]) => !dashboardBreakpointOrder.includes(breakpoint as DashboardBreakpoint))) {
    throw new Error("Dashboard 至少需要一个合法断点布局");
  }
  const cardIDs = new Set(spec.cards);
  for (const [breakpoint, items] of layouts as [DashboardBreakpoint, readonly DashboardGridItem[]][]) validateLayout(breakpoint, items, cardIDs, columns[breakpoint]);
  return freezeDashboardSpec(spec, layouts);
}

function unknownBreakpoints(values: Readonly<Partial<Record<DashboardBreakpoint, number>>> | undefined): boolean {
  return Object.keys(values ?? {}).some((key) => !dashboardBreakpointOrder.includes(key as DashboardBreakpoint));
}

function validateBreakpoints(columns: Readonly<Record<DashboardBreakpoint, number>>, widths: Readonly<Record<DashboardBreakpoint, number>>): void {
  let previous = -1;
  for (const breakpoint of dashboardBreakpointOrder) {
    validateInteger(columns[breakpoint], 1, 24, `Dashboard ${breakpoint} columns`);
    validateInteger(widths[breakpoint], 0, 10000, `Dashboard ${breakpoint} width`);
    if (widths[breakpoint] <= previous) throw new Error("Dashboard breakpoints 必须严格递增");
    previous = widths[breakpoint];
  }
}

function validateLayout(breakpoint: DashboardBreakpoint, items: readonly DashboardGridItem[], cardIDs: ReadonlySet<string>, columns: number): void {
  if (!Array.isArray(items)) throw new Error(`Dashboard ${breakpoint} 布局必须是数组`);
  const seen = new Set<string>();
  for (const item of items) {
    if (!cardIDs.has(item.cardID) || seen.has(item.cardID)) throw new Error(`Dashboard ${breakpoint} 引用了未知或重复卡片 ${item.cardID}`);
    seen.add(item.cardID);
    validateItem(item, columns);
  }
  if (seen.size !== cardIDs.size) throw new Error(`Dashboard ${breakpoint} 必须布局全部卡片`);
  validateCollisions(items, breakpoint);
}

function validateItem(item: DashboardGridItem, columns: number): void {
  validateInteger(item.x, 0, columns - 1, `Dashboard card ${item.cardID} x`);
  validateInteger(item.y, 0, 10000, `Dashboard card ${item.cardID} y`);
  validateInteger(item.width, 1, columns, `Dashboard card ${item.cardID} width`);
  validateInteger(item.height, 1, 1000, `Dashboard card ${item.cardID} height`);
  if (item.x + item.width > columns) throw new Error(`Dashboard card ${item.cardID} 超出列边界`);
  for (const [name, value, max] of [["minWidth", item.minWidth, columns], ["maxWidth", item.maxWidth, columns], ["minHeight", item.minHeight, 1000], ["maxHeight", item.maxHeight, 1000]] as const) {
    if (value !== undefined) validateInteger(value, 1, max, `Dashboard card ${item.cardID} ${name}`);
  }
  if ((item.minWidth !== undefined && item.maxWidth !== undefined && item.minWidth > item.maxWidth) || (item.minHeight !== undefined && item.maxHeight !== undefined && item.minHeight > item.maxHeight)) {
    throw new Error(`Dashboard card ${item.cardID} 的尺寸约束冲突`);
  }
  if ((item.minWidth !== undefined && item.width < item.minWidth) || (item.maxWidth !== undefined && item.width > item.maxWidth) || (item.minHeight !== undefined && item.height < item.minHeight) || (item.maxHeight !== undefined && item.height > item.maxHeight)) {
    throw new Error(`Dashboard card ${item.cardID} 的初始尺寸不满足约束`);
  }
  if (item.static !== undefined && typeof item.static !== "boolean") throw new Error(`Dashboard card ${item.cardID} static 无效`);
}

function validateCollisions(items: readonly DashboardGridItem[], breakpoint: DashboardBreakpoint): void {
  for (let leftIndex = 0; leftIndex < items.length; leftIndex += 1) {
    const left = items[leftIndex]!;
    for (let rightIndex = leftIndex + 1; rightIndex < items.length; rightIndex += 1) {
      const right = items[rightIndex]!;
      if (left.x < right.x + right.width && left.x + left.width > right.x && left.y < right.y + right.height && left.y + left.height > right.y) {
        throw new Error(`Dashboard ${breakpoint} 卡片布局重叠: ${left.cardID}/${right.cardID}`);
      }
    }
  }
}

function freezeDashboardSpec(spec: DashboardGridSpec, layouts: [string, readonly DashboardGridItem[]][]): DashboardGridSpec {
  const frozenLayouts = Object.fromEntries(layouts.map(([breakpoint, items]) => [breakpoint, Object.freeze(items.map((item) => Object.freeze({ ...item })))]));
  return Object.freeze({
    ...spec,
    cards: Object.freeze([...spec.cards]),
    layouts: Object.freeze(frozenLayouts),
    ...(spec.columns === undefined ? {} : { columns: Object.freeze({ ...spec.columns }) }),
    ...(spec.breakpoints === undefined ? {} : { breakpoints: Object.freeze({ ...spec.breakpoints }) }),
    ...(spec.gap === undefined ? {} : { gap: Object.freeze({ ...spec.gap }) }),
  });
}

function validateInteger(value: number, minimum: number, maximum: number, label: string): void {
  if (!Number.isSafeInteger(value) || value < minimum || value > maximum) throw new Error(`${label} 无效`);
}

function validID(value: unknown): value is string {
  return typeof value === "string" && /^[A-Za-z0-9][A-Za-z0-9._-]{0,159}$/.test(value);
}
