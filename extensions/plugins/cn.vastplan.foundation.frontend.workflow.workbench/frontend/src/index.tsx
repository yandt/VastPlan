import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { ActionSpec, CollectionDensity, CollectionSpec, FilterSpec } from "@vastplan/ui-contract";
import { jsonSchemaDialect, message, usePortalI18n, usePortalUI, type FormSchema, type UIWorkbenchAdapter } from "@vastplan/ui-primitives";
import type { CollectionActionContext, CollectionPageDefinition, CollectionQuery, WorkbenchPresentationConfig } from "@vastplan/workbench-sdk";

const namespace = "cn.vastplan.foundation.frontend.workflow.workbench";
type Row = Record<string, unknown>;

function CollectionPage({ page, preferenceScope, presentation }: { page: CollectionPageDefinition; preferenceScope: string; presentation?: WorkbenchPresentationConfig }) {
  const ui = usePortalUI();
  const i18n = usePortalI18n();
  const collection = page.collection;
  const density = collectionDensity(collection, presentation);
  const [filters, setFilters] = useState<Record<string, unknown>>({});
  const [pageNumber, setPageNumber] = useState(1);
  const [pageSize, setPageSize] = useState(collection.query.defaultPageSize);
  const [rows, setRows] = useState<readonly Row[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [failure, setFailure] = useState<string>();
  const [selectedKeys, setSelectedKeys] = useState<readonly string[]>([]);
  const [preferencesOpen, setPreferencesOpen] = useState(false);
  const [columns, setColumns] = useState(() => readColumns(preferenceScope, collection));
  const requestRef = useRef<AbortController | undefined>(undefined);
  const keyOf = useCallback((row: Row) => String(row.id ?? row.key ?? ""), []);
  const selected = useMemo(() => rows.filter((row) => selectedKeys.includes(keyOf(row))), [keyOf, rows, selectedKeys]);

  useEffect(() => { writeColumns(preferenceScope, collection, columns); }, [collection, columns, preferenceScope]);

  const request = useCallback(async (signal: AbortSignal, background = false) => {
    background ? setRefreshing(true) : setLoading(true);
    try {
      const query: CollectionQuery = { page: pageNumber, pageSize, filters };
      const result = await page.load(query, signal);
      if (signal.aborted) return;
      setRows(result.items as readonly Row[]);
      setTotal(result.total);
      setSelectedKeys([]);
      setFailure(undefined);
    } catch (error) {
      if (!signal.aborted) setFailure(error instanceof Error ? error.message : String(error));
    } finally {
      if (!signal.aborted) { setLoading(false); setRefreshing(false); }
    }
  }, [filters, page, pageNumber, pageSize]);

  const startRequest = useCallback((background = false) => {
    requestRef.current?.abort();
    const controller = new AbortController();
    requestRef.current = controller;
    void request(controller.signal, background);
  }, [request]);
  useEffect(() => { startRequest(); return () => requestRef.current?.abort(); }, [startRequest]);
  const refresh = useCallback(() => { startRequest(rows.length > 0); }, [rows.length, startRequest]);
  const runAction = useCallback(async (action: ActionSpec, actionRows: readonly Row[]) => {
    if (action.requiresSelection && actionRows.length === 0) return;
    const title = i18n.text(action.label);
    if (action.confirm !== undefined && !await ui.confirm({ title, content: i18n.text(action.confirm) })) return;
    try {
      const context: CollectionActionContext = { action, selected: actionRows, refresh };
      await page.runAction?.(context, new AbortController().signal);
      refresh();
    } catch (error) {
      ui.notify({ title, content: error instanceof Error ? error.message : String(error), kind: "error" });
    }
  }, [i18n, page, refresh, ui]);

  const visibleColumns = columns.filter((column) => column.visible).map((column) => collection.columns.find((candidate) => candidate.key === column.key)).filter((column): column is NonNullable<typeof column> => column !== undefined);
  const rowActions = (collection.actions ?? []).filter((action) => action.placement === "record.row");
  const tableColumns = [...visibleColumns.map((column) => ({ key: column.key, title: i18n.text(column.label), width: column.minWidth })), ...(rowActions.length === 0 ? [] : [{ key: "__actions", title: i18n.text(message(namespace, "column.actions", "操作")), render: (_value: unknown, row: Row) => <ui.Stack direction="row" gap="xs" wrap>{rowActions.map((action) => <ui.Button key={action.id} kind={action.tone ?? "text"} onClick={() => void runAction(action, [row])}>{i18n.text(action.label)}</ui.Button>)}</ui.Stack> }])];
  const toolbarActions = (collection.actions ?? []).filter((action) => action.placement === "page.primary" || action.placement === "page.secondary" || action.placement === "collection.toolbar");
  const bulkActions = (collection.actions ?? []).filter((action) => action.placement === "collection.bulk");

  return <ui.Stack gap={density === "compact" ? "sm" : density === "comfortable" ? "lg" : "md"}>
    {collection.filters === undefined || collection.filters.length === 0 ? null : <ui.FilterBar actions={<ui.Stack direction="row" gap="sm"><ui.Button kind="text" onClick={() => { setFilters({}); setPageNumber(1); }}>{i18n.text(message(namespace, "action.clearFilters", "清除筛选"))}</ui.Button><ui.Button kind="secondary" onClick={refresh} loading={refreshing}>{i18n.text(message(namespace, "action.refresh", "刷新"))}</ui.Button></ui.Stack>}><ui.FormRenderer schema={filterSchema(collection.filters)} value={filters} onChange={(value) => { setFilters(value); setPageNumber(1); }} /></ui.FilterBar>}
    <ui.Stack direction="row" gap="sm" wrap justify="end">
      {collection.filters === undefined || collection.filters.length === 0 ? <ui.Button kind="secondary" onClick={refresh} loading={refreshing}>{i18n.text(message(namespace, "action.refresh", "刷新"))}</ui.Button> : null}
      <ui.Button kind="secondary" onClick={() => setPreferencesOpen(true)}>{i18n.text(message(namespace, "action.columns", "列设置"))}</ui.Button>
      {toolbarActions.map((action) => <ui.Button key={action.id} kind={action.tone ?? "secondary"} disabled={Boolean(action.requiresSelection && selected.length === 0)} onClick={() => void runAction(action, selected)}>{i18n.text(action.label)}</ui.Button>)}
    </ui.Stack>
    {bulkActions.length === 0 ? null : <ui.Stack direction="row" gap="sm" wrap><span>{i18n.text(message(namespace, "selection.count", "已选择 {count} 项", { count: selected.length }))}</span>{bulkActions.map((action) => <ui.Button key={action.id} kind={action.tone ?? "secondary"} disabled={selected.length === 0} onClick={() => void runAction(action, selected)}>{i18n.text(action.label)}</ui.Button>)}</ui.Stack>}
    {failure === undefined ? null : <ui.ErrorState title={failure} retry={refresh} />}
    <ui.Table columns={tableColumns} rows={rows} rowKey={keyOf} selection={collection.selection ?? "none"} selectedRowKeys={selectedKeys} onSelectionChange={setSelectedKeys} loading={loading} density={density} empty={<ui.EmptyState title={i18n.text(message(namespace, "empty.title", "暂无数据"))} />} />
    <ui.Pagination page={pageNumber} pageSize={pageSize} total={total} disabled={loading} onChange={(nextPage, nextSize) => { setPageNumber(nextPage); setPageSize(nextSize); }} />
    <ui.Dialog open={preferencesOpen} title={i18n.text(message(namespace, "columns.title", "列设置"))} onClose={() => setPreferencesOpen(false)} footer={<ui.Button kind="primary" onClick={() => setPreferencesOpen(false)}>{i18n.text(message(namespace, "action.done", "完成"))}</ui.Button>}><ui.Stack gap="sm">{columns.map((column, index) => <ui.Stack key={column.key} direction="row" gap="sm" align="center"><ui.Button kind="text" onClick={() => setColumns(move(columns, index, -1))} disabled={index === 0}>↑</ui.Button><ui.Button kind="text" onClick={() => setColumns(move(columns, index, 1))} disabled={index === columns.length - 1}>↓</ui.Button><ui.Button kind={column.visible ? "secondary" : "text"} onClick={() => setColumns(columns.map((item) => item.key === column.key ? { ...item, visible: !item.visible } : item))}>{column.visible ? i18n.text(message(namespace, "action.hide", "隐藏")) : i18n.text(message(namespace, "action.show", "显示"))}</ui.Button><span>{i18n.text(collection.columns.find((item) => item.key === column.key)?.label ?? message(namespace, "column.unknown", "未知列"))}</span></ui.Stack>)}</ui.Stack></ui.Dialog>
  </ui.Stack>;
}

function collectionDensity(collection: CollectionSpec, presentation: WorkbenchPresentationConfig | undefined): CollectionDensity {
  const configured = collection.presentation?.density ?? presentation?.collection?.defaultDensity ?? "standard";
  const allowed = presentation?.collection?.allowedDensities;
  return allowed === undefined || allowed.includes(configured) ? configured : presentation?.collection?.defaultDensity ?? "standard";
}

function initialColumns(collection: CollectionSpec) { return collection.columns.map((column) => ({ key: column.key, visible: column.defaultVisible !== false })); }
function preferencesKey(scope: string, collection: CollectionSpec) { return `vastplan.workbench.columns.${scope}.${collection.id}`; }
function readColumns(scope: string, collection: CollectionSpec) {
  const fallback = initialColumns(collection);
  try {
    const raw = globalThis.localStorage?.getItem(preferencesKey(scope, collection));
    if (raw === null || raw === undefined) return fallback;
    const parsed = JSON.parse(raw) as unknown;
    if (!Array.isArray(parsed)) return fallback;
    const allowed = new Set(collection.preferences?.allowedColumns ?? collection.columns.map((column) => column.key));
    const restored = parsed.flatMap((item) => typeof item === "object" && item !== null && typeof (item as { key?: unknown }).key === "string" && allowed.has((item as { key: string }).key) ? [{ key: (item as { key: string }).key, visible: (item as { visible?: unknown }).visible !== false }] : []);
    const missing = fallback.filter((column) => !restored.some((item) => item.key === column.key));
    return [...restored, ...missing];
  } catch { return fallback; }
}
function writeColumns(scope: string, collection: CollectionSpec, columns: readonly { key: string; visible: boolean }[]) {
  try { globalThis.localStorage?.setItem(preferencesKey(scope, collection), JSON.stringify(columns)); } catch { /* Browser privacy mode may reject local preference storage. */ }
}
function move<T>(items: readonly T[], index: number, offset: number): T[] { const target = index + offset; if (target < 0 || target >= items.length) return [...items]; const copy = [...items]; const [item] = copy.splice(index, 1); copy.splice(target, 0, item!); return copy; }
function filterSchema(filters: readonly FilterSpec[]): FormSchema { return { id: "workbench.collection.filters", schema: { $schema: jsonSchemaDialect, type: "object", properties: Object.fromEntries(filters.map((filter) => [filter.id, filterProperty(filter)])) } } as unknown as FormSchema; }
function filterProperty(filter: FilterSpec) { if (filter.kind === "select") return { type: "string", oneOf: (filter.options ?? []).map((option) => ({ const: option.value, title: option.value })) }; if (filter.kind === "boolean") return { type: "boolean" }; if (filter.kind === "numberRange") return { type: "object", properties: { from: { type: "number" }, to: { type: "number" } } }; if (filter.kind === "dateRange") return { type: "object", properties: { from: { type: "string", format: "date" }, to: { type: "string", format: "date" } } }; return { type: "string" }; }

export const workbench: UIWorkbenchAdapter = { id: "ui.workflow.workbench", uiContract: "3.0.0", CollectionPage, localization: { defaultLocale: "zh-CN", messages: { "zh-CN": { "action.refresh": "刷新", "action.clearFilters": "清除筛选", "action.columns": "列设置", "column.actions": "操作", "selection.count": "已选择 {count} 项", "empty.title": "暂无数据", "columns.title": "列设置", "action.done": "完成", "action.hide": "隐藏", "action.show": "显示", "column.unknown": "未知列" }, "en-US": { "action.refresh": "Refresh", "action.clearFilters": "Clear filters", "action.columns": "Columns", "column.actions": "Actions", "selection.count": "{count} selected", "empty.title": "No data", "columns.title": "Columns", "action.done": "Done", "action.hide": "Hide", "action.show": "Show", "column.unknown": "Unknown column" } } } };
export default workbench;
