import { useCallback, useEffect, useMemo, useRef, useState, useSyncExternalStore } from "react";
import type { ActionSpec, CollectionDensity } from "@vastplan/ui-contract";
import { ComponentSizeProvider, usePortalI18n, usePortalUI, type PageRefreshSignal, type WorkbenchPreferencePort } from "@vastplan/ui-primitives";
import type { CollectionActionContext, CollectionPageDefinition, CollectionSummary as CollectionSummaryDefinition, WorkbenchPresentationConfig } from "@vastplan/workbench-sdk";
import { CollectionCards } from "./CollectionCards.js";
import { FilterPanel } from "../filter/FilterPanel.js";
import { CollectionPreferencesPopover } from "./CollectionPreferencesPopover.js";
import { CollectionTable } from "./CollectionTable.js";
import { CollectionToolbar } from "./CollectionToolbar.js";
import { CollectionSummary } from "./CollectionSummary.js";
import { collectionDensity, collectionDensityOptions } from "./density.js";
import { collectionSelectionMode, type CollectionRow } from "./model.js";
import { collectionPreferenceFromColumns, readCollectionColumns, writeCollectionColumns } from "./preferences.js";
import { useCollectionData } from "./useCollectionData.js";
import { CollectionFormWorkflow } from "../form/CollectionFormWorkflow.js";
import { evaluateFormCondition } from "../form/presentation.js";
import { CollectionOverlayWorkflow } from "../overlay/CollectionOverlayWorkflow.js";
import { WorkbenchPageFlow } from "../layout/WorkbenchRhythm.js";
import { useActionExecution } from "../action/ActionExecutionFeedback.js";

const emptyRefreshSignal: PageRefreshSignal = Object.freeze({ subscribe: () => () => undefined, getSnapshot: () => 0 });

export function CollectionPage({ page, preferenceScope, preferences, presentation, refreshSignal = emptyRefreshSignal }: { page: CollectionPageDefinition; preferenceScope: string; preferences?: WorkbenchPreferencePort; presentation?: WorkbenchPresentationConfig; refreshSignal?: PageRefreshSignal }) {
  const ui = usePortalUI();
  const i18n = usePortalI18n();
  const collection = page.collection;
  const initialPreference = preferences?.readCollection(collection.id);
  const densityOptions = collectionDensityOptions(collection, presentation);
  const [filters, setFilters] = useState<Record<string, unknown>>({});
  const [pageNumber, setPageNumber] = useState(1);
  const [pageSize, setPageSize] = useState(() => validPageSize(collection, initialPreference?.pageSize));
  const [density, setDensity] = useState(() => collectionDensity(collection, presentation, initialPreference?.density));
  const [summary, setSummary] = useState<CollectionSummaryDefinition>();
  const [summaryFailure, setSummaryFailure] = useState<string>();
  const [selectedKeys, setSelectedKeys] = useState<readonly string[]>([]);
  const [columns, setColumns] = useState(() => readCollectionColumns(preferenceScope, collection, initialPreference));
  const [activeForm, setActiveForm] = useState<{ id: string; selected: readonly CollectionRow[] }>();
  const [activeOverlay, setActiveOverlay] = useState<{ id: string; selected: readonly CollectionRow[] }>();
  const execution = useActionExecution();
  const summaryRequestRef = useRef<AbortController>();
  const preferenceMutationRef = useRef(0);
  const keyOf = useCallback((row: CollectionRow) => String(row.id ?? row.key ?? ""), []);
  const data = useCollectionData({ page, pageNumber, pageSize, filters, keyOf });
  const refreshRevision = useSyncExternalStore(refreshSignal.subscribe, refreshSignal.getSnapshot, refreshSignal.getSnapshot);
  const { rows } = data;
  const selected = useMemo(() => rows.filter((row) => selectedKeys.includes(keyOf(row))), [keyOf, rows, selectedKeys]);

  const persistPreference = useCallback((nextColumns: typeof columns, nextDensity: CollectionDensity, nextPageSize: number, rollback: () => void) => {
    if (preferences === undefined) {
      writeCollectionColumns(preferenceScope, collection, nextColumns);
      return;
    }
    const mutation = ++preferenceMutationRef.current;
    const next = collectionPreferenceFromColumns(nextColumns, { ...preferences.readCollection(collection.id), density: nextDensity, pageSize: nextPageSize });
    void preferences.writeCollection(collection.id, next).catch((error) => {
      if (preferenceMutationRef.current === mutation) rollback();
      ui.notify({ title: i18n.text({ namespace: "cn.vastplan.foundation.frontend.workflow.workbench", key: "preference.saveFailed", fallback: "显示偏好保存失败" }), content: error instanceof Error ? error.message : String(error), kind: "error" });
    });
  }, [collection, i18n, preferenceScope, preferences, ui]);
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
  useEffect(() => { if (refreshRevision > 0) { data.refresh(); startSummaryRequest(); } }, [data.refresh, refreshRevision, startSummaryRequest]);
  useEffect(() => { setSelectedKeys([]); }, [data.resetToken]);
  const refresh = useCallback(() => { data.refresh(); startSummaryRequest(); }, [data.refresh, startSummaryRequest]);
  const runAction = useCallback(async (action: ActionSpec, actionRows: readonly CollectionRow[]) => {
    if (execution.running) return;
    if (action.requiresSelection && actionRows.length === 0) return;
    if (action.form !== undefined) {
      const definition = page.forms?.find((form) => form.id === action.form);
      if (definition === undefined) { ui.notify({ title: i18n.text(action.label), content: `未注册表单 ${action.form}`, kind: "error" }); return; }
      setActiveForm({ id: definition.id, selected: actionRows });
      return;
    }
    if (action.overlay !== undefined) {
      const definition = page.overlays?.find((overlay) => overlay.id === action.overlay);
      if (definition === undefined) { ui.notify({ title: i18n.text(action.label), content: `未注册 Overlay ${action.overlay}`, kind: "error" }); return; }
      setActiveOverlay({ id: definition.id, selected: actionRows });
      return;
    }
    const title = i18n.text(action.label);
    if (action.confirm !== undefined && !await ui.confirm({ title, content: i18n.text(action.confirm) })) return;
    try {
      const context: CollectionActionContext = { action, selected: actionRows, refresh };
      const outcome = await execution.run({ id: action.id, label: title }, (signal) => page.runAction?.(context, signal) ?? Promise.resolve(undefined));
      if (!outcome.completed) return;
      const result = outcome.value;
      if (result?.notify !== undefined) ui.notify({ title: i18n.text(result.notify.title), content: result.notify.content === undefined ? undefined : i18n.text(result.notify.content), kind: result.notify.kind });
      refresh();
    } catch (error) {
      ui.notify({ title, content: error instanceof Error ? error.message : String(error), kind: "error" });
    }
  }, [execution, i18n, page, refresh, ui]);
  const actions = collection.actions ?? [];
  const visibleAction = (action: ActionSpec) => action.visibleWhen === undefined || (selected[0] !== undefined && evaluateFormCondition(action.visibleWhen, selected[0]));
  const updatePreferences = useCallback((nextColumns: typeof columns, nextDensity: CollectionDensity) => {
    const previousColumns = columns;
    const previousDensity = density;
    setColumns([...nextColumns]);
    setDensity(nextDensity);
    persistPreference([...nextColumns], nextDensity, pageSize, () => { setColumns(previousColumns); setDensity(previousDensity); });
  }, [columns, density, pageSize, persistPreference]);
  const toolbarActions = actions.filter((action) => action.placement === "collection.toolbar" && visibleAction(action));
  const bulkActions = actions.filter((action) => action.placement === "collection.bulk" && visibleAction(action));
  const selectionMode = collectionSelectionMode(actions, collection.selection);
  const hasFilters = collection.filterPanel !== undefined;

  return <WorkbenchPageFlow size={page.size}><ComponentSizeProvider size={collection.size}>
    <ui.Stack gap="sm">
      {summary === undefined ? null : <CollectionSummary summary={summary} />}
      {hasFilters ? <div style={{ width: "100%", minWidth: 0 }}><FilterPanel panel={collection.filterPanel!} value={filters} querying={data.loading || data.refreshing || data.loadingMore} size="xs" onApply={(value) => { setFilters(value); setPageNumber(1); }} /></div> : null}
      <div style={{ width: "100%", minWidth: 0 }}><CollectionToolbar hasFilters={hasFilters} refreshing={data.refreshing} selectedCount={selected.length} toolbarActions={toolbarActions} bulkActions={bulkActions} runningActionID={execution.active?.id} actionRunning={execution.running} onRefresh={refresh} preferences={collection.view === "table" && collection.preferences !== undefined ? <CollectionPreferencesPopover collection={collection} columns={columns} density={density} densityOptions={densityOptions} onChange={updatePreferences} /> : undefined} onRunAction={(action) => void runAction(action, selected)} /></div>
    </ui.Stack>
    {data.failure === undefined && summaryFailure === undefined ? null : <div style={{ width: "100%", minWidth: 0 }}><ui.ErrorState title={data.failure ?? summaryFailure!} retry={refresh} /></div>}
    <div style={{ width: "100%", minWidth: 0 }}>{collection.view === "cards"
      ? <CollectionCards collection={collection} selectionMode={selectionMode} rows={rows} selectedKeys={selectedKeys} loading={data.loading} loadingMore={data.loadingMore} nextCursor={data.nextCursor} keyOf={keyOf} onSelectionChange={setSelectedKeys} runningActionID={execution.active?.id} actionRunning={execution.running} onRunAction={(action, actionRows) => void runAction(action, actionRows)} onLoadMore={data.loadMore} />
      : <CollectionTable collection={collection} selectionMode={selectionMode} columns={columns} rows={rows} selectedKeys={selectedKeys} loading={data.loading} density={density} keyOf={keyOf} onSelectionChange={setSelectedKeys} runningActionID={execution.active?.id} actionRunning={execution.running} onRunAction={(action, actionRows) => void runAction(action, actionRows)} />}</div>
    {collection.query.mode !== "page" ? null : <div style={{ width: "100%", minWidth: 0 }}><ui.Pagination align="end" page={pageNumber} pageSize={pageSize} pageSizeOptions={collection.query.pageSizeOptions} total={data.total} disabled={data.loading} onChange={(nextPage, requestedSize) => {
      const nextSize = validPageSize(collection, requestedSize);
      const previousSize = pageSize;
      setPageNumber(nextPage);
      setPageSize(nextSize);
      if (nextSize !== previousSize) persistPreference(columns, density, nextSize, () => setPageSize(previousSize));
    }} /></div>}
    {collection.view !== "table" || collection.query.mode !== "cursor" || data.nextCursor === undefined ? null : <ui.Stack direction="row" justify="center"><ui.Button kind="secondary" loading={data.loadingMore} disabled={data.loadingMore} onClick={data.loadMore}>{i18n.text({ namespace: "cn.vastplan.foundation.frontend.workflow.workbench", key: "cursor.more", fallback: "加载更多" })}</ui.Button></ui.Stack>}
    <CollectionFormWorkflow definition={page.forms?.find((form) => form.id === activeForm?.id)} selected={activeForm?.selected ?? []} open={activeForm !== undefined} onClose={() => setActiveForm(undefined)} onRefresh={refresh} />
    <CollectionOverlayWorkflow definition={page.overlays?.find((overlay) => overlay.id === activeOverlay?.id)} selected={activeOverlay?.selected ?? []} open={activeOverlay !== undefined} onClose={() => setActiveOverlay(undefined)} />
    {execution.feedback}
  </ComponentSizeProvider></WorkbenchPageFlow>;
}

function validPageSize(collection: CollectionPageDefinition["collection"], preferred: number | undefined): number {
  return preferred !== undefined && collection.query.pageSizeOptions.includes(preferred) ? preferred : collection.query.defaultPageSize;
}
