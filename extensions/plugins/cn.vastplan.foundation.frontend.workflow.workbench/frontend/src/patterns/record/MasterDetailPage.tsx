import { useCallback, useEffect, useMemo, useState } from "react";
import { message, usePortalI18n, usePortalUI } from "@vastplan/ui-primitives";
import type { MasterDetailPageDefinition } from "@vastplan/workbench-sdk";
import { CollectionFilters } from "../collection/CollectionFilters.js";
import { useCollectionData, type CollectionDataSource } from "../collection/useCollectionData.js";
import { initialRecordSelection, persistRecordSelection } from "./selection.js";
import { RecordWorkspace } from "./RecordWorkspace.js";
import { useNarrowSplitView } from "./useNarrowSplitView.js";
import { useRecordLoader } from "./useRecordLoader.js";

const namespace = "cn.vastplan.foundation.frontend.workflow.workbench";

export function MasterDetailPage({ page }: { page: MasterDetailPageDefinition }) {
  const ui = usePortalUI();
  const i18n = usePortalI18n();
  const parameter = page.master.selectionParam ?? page.master.id;
  const [selected, setSelected] = useState(() => initialRecordSelection(parameter));
  const [filters, setFilters] = useState<Record<string, unknown>>({});
  const [pageNumber, setPageNumber] = useState(1);
  const [pageSize, setPageSize] = useState(page.master.query.defaultPageSize);
  const [dirty, setDirty] = useState(false);
  const [showPrimary, setShowPrimary] = useState(selected === undefined);
  const narrow = useNarrowSplitView();
  const source = useMemo<CollectionDataSource>(() => ({ collection: { query: page.master.query }, load: page.loadMaster }), [page]);
  const keyOf = useCallback((row: Record<string, unknown>) => String(row[page.master.keyField] ?? ""), [page.master.keyField]);
  const master = useCollectionData({ page: source, pageNumber, pageSize, filters, keyOf });
  useEffect(() => {
    if (selected !== undefined || master.loading || master.rows.length === 0) return;
    const first = keyOf(master.rows[0]);
    if (first !== "") { setSelected(first); persistRecordSelection(parameter, first); }
  }, [keyOf, master.loading, master.rows, parameter, selected]);
  const loadRecord = useCallback((key: string, signal: AbortSignal) => page.loadRecord(key, signal), [page]);
  const data = useRecordLoader(selected, loadRecord);
  const refresh = useCallback(() => { master.refresh(); data.refresh(); }, [data.refresh, master.refresh]);
  const choose = async (id: string) => {
    if (id === selected) { if (narrow) setShowPrimary(false); return; }
    if (dirty && !await ui.confirm({ title: i18n.text(message(namespace, "form.discardTitle", "放弃未保存的修改？")), content: i18n.text(message(namespace, "record.selectionDiscard", "切换记录后，当前未保存修改不会保留。")) })) return;
    setSelected(id); persistRecordSelection(parameter, id); setShowPrimary(false);
  };
  const items = master.rows.map((row) => ({
    id: keyOf(row), title: String(row[page.master.titleField] ?? ""),
    description: page.master.subtitleField === undefined ? undefined : String(row[page.master.subtitleField] ?? ""),
    status: page.master.status === undefined ? undefined : <ui.Status tone={tone(page.master.status.toneField === undefined ? undefined : row[page.master.status.toneField])}>{String(row[page.master.status.labelField] ?? "")}</ui.Status>,
  }));
  const primary = <ui.Stack gap="sm">
    {(page.master.filters?.length ?? 0) === 0 ? null : <CollectionFilters filters={page.master.filters!} value={filters} querying={master.loading || master.refreshing} onApply={(next) => { setFilters(next); setPageNumber(1); }} />}
    <ui.Stack direction="row" align="center" justify="between"><strong>{i18n.text(page.master.title)}</strong><ui.IconButton icon="refresh" label={i18n.text(message(namespace, "action.refresh", "刷新"))} loading={master.refreshing} onClick={master.refresh} /></ui.Stack>
    {master.failure === undefined ? null : <ui.ErrorState title={master.failure} retry={master.refresh} />}
    {master.loading ? <ui.Skeleton rows={6} /> : items.length === 0 ? <ui.EmptyState title={i18n.text(page.master.emptyTitle ?? message(namespace, "record.masterEmpty", "暂无记录"))} /> : <ui.RecordNavigationList items={items} selectedID={selected} ariaLabel={i18n.text(page.master.title)} onSelect={(id) => void choose(id)} />}
    {page.master.query.mode !== "page" ? null : <ui.Pagination page={pageNumber} pageSize={pageSize} total={master.total} disabled={master.loading} onChange={(nextPage, nextSize) => { setPageNumber(nextPage); setPageSize(nextSize); }} />}
    {page.master.query.mode !== "cursor" || master.nextCursor === undefined ? null : <ui.Button kind="secondary" loading={master.loadingMore} onClick={master.loadMore}>{i18n.text(message(namespace, "cursor.more", "加载更多"))}</ui.Button>}
  </ui.Stack>;
  return <RecordWorkspace page={page} data={data} refresh={refresh} primary={primary} primaryLabel={i18n.text(page.master.title)} splitMode={narrow ? showPrimary ? "primary" : "secondary" : "both"} onBack={narrow ? () => setShowPrimary(true) : undefined} onDirtyChange={setDirty} />;
}

function tone(value: unknown): "neutral" | "info" | "success" | "warning" | "error" { return value === "info" || value === "success" || value === "warning" || value === "error" ? value : "neutral"; }
