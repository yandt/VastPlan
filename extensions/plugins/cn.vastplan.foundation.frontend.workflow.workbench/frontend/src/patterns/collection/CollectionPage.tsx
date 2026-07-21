import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { ActionSpec } from "@vastplan/ui-contract";
import { usePortalI18n, usePortalUI } from "@vastplan/ui-primitives";
import type { CollectionActionContext, CollectionPageDefinition, CollectionQuery, CollectionSummary, WorkbenchPresentationConfig } from "@vastplan/workbench-sdk";
import { CollectionFilters } from "./CollectionFilters.js";
import { CollectionPreferencesDialog } from "./CollectionPreferencesDialog.js";
import { CollectionTable } from "./CollectionTable.js";
import { CollectionToolbar } from "./CollectionToolbar.js";
import { collectionDensity } from "./density.js";
import type { CollectionRow } from "./model.js";
import { readCollectionColumns, writeCollectionColumns } from "./preferences.js";

export function CollectionPage({ page, preferenceScope, presentation }: { page: CollectionPageDefinition; preferenceScope: string; presentation?: WorkbenchPresentationConfig }) {
  const ui = usePortalUI();
  const i18n = usePortalI18n();
  const collection = page.collection;
  const density = collectionDensity(collection, presentation);
  const [filters, setFilters] = useState<Record<string, unknown>>({});
  const [pageNumber, setPageNumber] = useState(1);
  const [pageSize, setPageSize] = useState(collection.query.defaultPageSize);
  const [rows, setRows] = useState<readonly CollectionRow[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [failure, setFailure] = useState<string>();
  const [summary, setSummary] = useState<CollectionSummary>();
  const [summaryFailure, setSummaryFailure] = useState<string>();
  const [selectedKeys, setSelectedKeys] = useState<readonly string[]>([]);
  const [preferencesOpen, setPreferencesOpen] = useState(false);
  const [columns, setColumns] = useState(() => readCollectionColumns(preferenceScope, collection));
  const requestRef = useRef<AbortController>();
  const summaryRequestRef = useRef<AbortController>();
  const keyOf = useCallback((row: CollectionRow) => String(row.id ?? row.key ?? ""), []);
  const selected = useMemo(() => rows.filter((row) => selectedKeys.includes(keyOf(row))), [keyOf, rows, selectedKeys]);

  useEffect(() => { writeCollectionColumns(preferenceScope, collection, columns); }, [collection, columns, preferenceScope]);
  const request = useCallback(async (signal: AbortSignal, background = false) => {
    background ? setRefreshing(true) : setLoading(true);
    try {
      const query: CollectionQuery = { page: pageNumber, pageSize, filters };
      const result = await page.load(query, signal);
      if (signal.aborted) return;
      setRows(result.items as readonly CollectionRow[]);
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
  const requestSummary = useCallback(async (signal: AbortSignal) => {
    if (page.loadSummary === undefined) { setSummary(undefined); setSummaryFailure(undefined); return; }
    try {
      const next = await page.loadSummary(signal);
      if (!signal.aborted) { setSummary(next); setSummaryFailure(undefined); }
    } catch (error) {
      if (!signal.aborted) setSummaryFailure(error instanceof Error ? error.message : String(error));
    }
  }, [page]);
  const startSummaryRequest = useCallback(() => {
    summaryRequestRef.current?.abort();
    const controller = new AbortController();
    summaryRequestRef.current = controller;
    void requestSummary(controller.signal);
  }, [requestSummary]);
  useEffect(() => { startRequest(); return () => requestRef.current?.abort(); }, [startRequest]);
  useEffect(() => { startSummaryRequest(); return () => summaryRequestRef.current?.abort(); }, [startSummaryRequest]);
  const refresh = useCallback(() => { startRequest(rows.length > 0); startSummaryRequest(); }, [rows.length, startRequest, startSummaryRequest]);
  const runAction = useCallback(async (action: ActionSpec, actionRows: readonly CollectionRow[]) => {
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
  const actions = collection.actions ?? [];
  const primaryActions = actions.filter((action) => action.placement === "page.primary" || action.placement === "collection.toolbar");
  const secondaryActions = actions.filter((action) => action.placement === "page.secondary");
  const bulkActions = actions.filter((action) => action.placement === "collection.bulk");
  const hasFilters = collection.filters !== undefined && collection.filters.length > 0;

  return <ui.Stack gap={density === "compact" ? "sm" : density === "comfortable" ? "lg" : "md"}>
    {summary === undefined ? null : <div style={{ width: "100%", minWidth: 0 }}><ui.Panel title={summary.title === undefined ? undefined : i18n.text(summary.title)}><ui.Descriptions columns={{ xs: 1, sm: 1, md: 2, lg: 2, xl: 3 }} items={summary.metrics.map((metric) => ({ id: metric.id, label: i18n.text(metric.label), value: metric.tone === undefined ? metric.value : <ui.Status tone={metric.tone}>{metric.value}</ui.Status> }))} /></ui.Panel></div>}
    {hasFilters ? <div style={{ width: "100%", minWidth: 0 }}><CollectionFilters filters={collection.filters!} value={filters} querying={loading || refreshing} onApply={(value) => { setFilters(value); setPageNumber(1); }} /></div> : null}
    <div style={{ width: "100%", minWidth: 0 }}><CollectionToolbar hasFilters={hasFilters} refreshing={refreshing} selectedCount={selected.length} primaryActions={primaryActions} secondaryActions={secondaryActions} bulkActions={bulkActions} onRefresh={refresh} onColumns={() => setPreferencesOpen(true)} onRunAction={(action) => void runAction(action, selected)} /></div>
    {failure === undefined && summaryFailure === undefined ? null : <div style={{ width: "100%", minWidth: 0 }}><ui.ErrorState title={failure ?? summaryFailure!} retry={refresh} /></div>}
    <div style={{ width: "100%", minWidth: 0 }}><CollectionTable collection={collection} columns={columns} rows={rows} selectedKeys={selectedKeys} loading={loading} density={density} keyOf={keyOf} onSelectionChange={setSelectedKeys} onRunAction={(action, actionRows) => void runAction(action, actionRows)} /></div>
    <div style={{ width: "100%", minWidth: 0 }}><ui.Pagination align="end" page={pageNumber} pageSize={pageSize} total={total} disabled={loading} onChange={(nextPage, nextSize) => { setPageNumber(nextPage); setPageSize(nextSize); }} /></div>
    <CollectionPreferencesDialog open={preferencesOpen} collection={collection} columns={columns} onChange={setColumns} onClose={() => setPreferencesOpen(false)} />
  </ui.Stack>;
}
