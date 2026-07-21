import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { ActionSpec } from "@vastplan/ui-contract";
import { usePortalI18n, usePortalUI } from "@vastplan/ui-primitives";
import type { CollectionActionContext, CollectionPageDefinition, CollectionSummary, WorkbenchPresentationConfig } from "@vastplan/workbench-sdk";
import { CollectionCards } from "./CollectionCards.js";
import { CollectionFilters } from "./CollectionFilters.js";
import { CollectionPreferencesDialog } from "./CollectionPreferencesDialog.js";
import { CollectionTable } from "./CollectionTable.js";
import { CollectionToolbar } from "./CollectionToolbar.js";
import { collectionDensity } from "./density.js";
import type { CollectionRow } from "./model.js";
import { readCollectionColumns, writeCollectionColumns } from "./preferences.js";
import { useCollectionData } from "./useCollectionData.js";
import { CollectionFormWorkflow } from "../form/CollectionFormWorkflow.js";

export function CollectionPage({ page, preferenceScope, presentation }: { page: CollectionPageDefinition; preferenceScope: string; presentation?: WorkbenchPresentationConfig }) {
  const ui = usePortalUI();
  const i18n = usePortalI18n();
  const collection = page.collection;
  const density = collectionDensity(collection, presentation);
  const [filters, setFilters] = useState<Record<string, unknown>>({});
  const [pageNumber, setPageNumber] = useState(1);
  const [pageSize, setPageSize] = useState(collection.query.defaultPageSize);
  const [summary, setSummary] = useState<CollectionSummary>();
  const [summaryFailure, setSummaryFailure] = useState<string>();
  const [selectedKeys, setSelectedKeys] = useState<readonly string[]>([]);
  const [preferencesOpen, setPreferencesOpen] = useState(false);
  const [columns, setColumns] = useState(() => readCollectionColumns(preferenceScope, collection));
  const [activeForm, setActiveForm] = useState<{ id: string; selected: readonly CollectionRow[] }>();
  const summaryRequestRef = useRef<AbortController>();
  const keyOf = useCallback((row: CollectionRow) => String(row.id ?? row.key ?? ""), []);
  const data = useCollectionData({ page, pageNumber, pageSize, filters, keyOf });
  const { rows } = data;
  const selected = useMemo(() => rows.filter((row) => selectedKeys.includes(keyOf(row))), [keyOf, rows, selectedKeys]);

  useEffect(() => { writeCollectionColumns(preferenceScope, collection, columns); }, [collection, columns, preferenceScope]);
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
  useEffect(() => { startSummaryRequest(); return () => summaryRequestRef.current?.abort(); }, [startSummaryRequest]);
  useEffect(() => { setSelectedKeys([]); }, [data.resetToken]);
  const refresh = useCallback(() => { data.refresh(); startSummaryRequest(); }, [data.refresh, startSummaryRequest]);
  const runAction = useCallback(async (action: ActionSpec, actionRows: readonly CollectionRow[]) => {
    if (action.requiresSelection && actionRows.length === 0) return;
    if (action.form !== undefined) {
      const definition = page.forms?.find((form) => form.id === action.form);
      if (definition === undefined) { ui.notify({ title: i18n.text(action.label), content: `未注册表单 ${action.form}`, kind: "error" }); return; }
      setActiveForm({ id: definition.id, selected: actionRows });
      return;
    }
    const title = i18n.text(action.label);
    if (action.confirm !== undefined && !await ui.confirm({ title, content: i18n.text(action.confirm) })) return;
    try {
      const context: CollectionActionContext = { action, selected: actionRows, refresh };
      const result = await page.runAction?.(context, new AbortController().signal);
      if (result?.notify !== undefined) ui.notify({ title: i18n.text(result.notify.title), content: result.notify.content === undefined ? undefined : i18n.text(result.notify.content), kind: result.notify.kind });
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
    {hasFilters ? <div style={{ width: "100%", minWidth: 0 }}><CollectionFilters filters={collection.filters!} value={filters} querying={data.loading || data.refreshing || data.loadingMore} onApply={(value) => { setFilters(value); setPageNumber(1); }} /></div> : null}
    <div style={{ width: "100%", minWidth: 0 }}><CollectionToolbar hasFilters={hasFilters} refreshing={data.refreshing} selectedCount={selected.length} primaryActions={primaryActions} secondaryActions={secondaryActions} bulkActions={bulkActions} onRefresh={refresh} onColumns={collection.view === "table" ? () => setPreferencesOpen(true) : undefined} onRunAction={(action) => void runAction(action, selected)} /></div>
    {data.failure === undefined && summaryFailure === undefined ? null : <div style={{ width: "100%", minWidth: 0 }}><ui.ErrorState title={data.failure ?? summaryFailure!} retry={refresh} /></div>}
    <div style={{ width: "100%", minWidth: 0 }}>{collection.view === "cards"
      ? <CollectionCards collection={collection} rows={rows} selectedKeys={selectedKeys} loading={data.loading} loadingMore={data.loadingMore} nextCursor={data.nextCursor} density={density} keyOf={keyOf} onSelectionChange={setSelectedKeys} onRunAction={(action, actionRows) => void runAction(action, actionRows)} onLoadMore={data.loadMore} />
      : <CollectionTable collection={collection} columns={columns} rows={rows} selectedKeys={selectedKeys} loading={data.loading} density={density} keyOf={keyOf} onSelectionChange={setSelectedKeys} onRunAction={(action, actionRows) => void runAction(action, actionRows)} />}</div>
    {collection.query.mode !== "page" ? null : <div style={{ width: "100%", minWidth: 0 }}><ui.Pagination align="end" page={pageNumber} pageSize={pageSize} total={data.total} disabled={data.loading} onChange={(nextPage, nextSize) => { setPageNumber(nextPage); setPageSize(nextSize); }} /></div>}
    {collection.view !== "table" || collection.query.mode !== "cursor" || data.nextCursor === undefined ? null : <ui.Stack direction="row" justify="center"><ui.Button kind="secondary" loading={data.loadingMore} disabled={data.loadingMore} onClick={data.loadMore}>{i18n.text({ namespace: "cn.vastplan.foundation.frontend.workflow.workbench", key: "cursor.more", fallback: "加载更多" })}</ui.Button></ui.Stack>}
    {collection.view !== "table" ? null : <CollectionPreferencesDialog open={preferencesOpen} collection={collection} columns={columns} onChange={setColumns} onClose={() => setPreferencesOpen(false)} />}
    <CollectionFormWorkflow definition={page.forms?.find((form) => form.id === activeForm?.id)} selected={activeForm?.selected ?? []} open={activeForm !== undefined} onClose={() => setActiveForm(undefined)} onRefresh={refresh} />
  </ui.Stack>;
}
