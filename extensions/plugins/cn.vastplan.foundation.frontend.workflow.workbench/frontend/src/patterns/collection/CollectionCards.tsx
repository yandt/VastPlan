import { useEffect, useRef, useState } from "react";
import type { ActionSpec, CollectionCardFieldSpec, CollectionDensity, CollectionSpec } from "@vastplan/ui-contract";
import { message, usePortalI18n, usePortalUI, type PortalI18n, type StatusTone } from "@vastplan/ui-primitives";
import type { CollectionRow } from "./model.js";

const namespace = "cn.vastplan.foundation.frontend.workflow.workbench";
const tones = new Set<StatusTone>(["neutral", "info", "success", "warning", "error"]);

export function CollectionCards({ collection, rows, selectedKeys, loading, loadingMore, nextCursor, density, keyOf, onSelectionChange, onRunAction, onLoadMore }: {
  collection: CollectionSpec;
  rows: readonly CollectionRow[];
  selectedKeys: readonly string[];
  loading: boolean;
  loadingMore: boolean;
  nextCursor?: string;
  density: CollectionDensity;
  keyOf(row: CollectionRow): string;
  onSelectionChange(keys: readonly string[]): void;
  onRunAction(action: ActionSpec, rows: readonly CollectionRow[]): void;
  onLoadMore(): void;
}) {
  const ui = usePortalUI();
  const i18n = usePortalI18n();
  const sentinel = useRef<HTMLDivElement>(null);
  const [viewportLoading, setViewportLoading] = useState(false);
  const card = collection.card;
  useEffect(() => {
    setViewportLoading(false);
    if (card?.loadMore !== "viewport" || nextCursor === undefined || typeof IntersectionObserver === "undefined" || sentinel.current === null) return;
    setViewportLoading(true);
    const observer = new IntersectionObserver((entries) => { if (entries.some((entry) => entry.isIntersecting)) onLoadMore(); }, { rootMargin: "240px" });
    observer.observe(sentinel.current);
    return () => observer.disconnect();
  }, [card?.loadMore, nextCursor, onLoadMore]);
  if (card === undefined) return <ui.ErrorState title="Card Collection 缺少 card 呈现契约" />;
  if (loading) return <ui.Skeleton rows={Math.max(3, collection.query.defaultPageSize)} />;
  if (rows.length === 0) return <ui.EmptyState title={i18n.text(message(namespace, "empty.title", "暂无数据"))} />;
  const footerActions = (collection.actions ?? []).filter((action) => action.placement === "card.footer" || action.placement === "record.row");
  const selected = new Set(selectedKeys);
  const select = (key: string, checked: boolean) => {
    if ((collection.selection ?? "none") === "single") { onSelectionChange(checked ? [key] : []); return; }
    const next = new Set(selected);
    checked ? next.add(key) : next.delete(key);
    onSelectionChange([...next]);
  };
  const columns = card.columns ?? { xs: 1, sm: 1, md: 2, lg: 3, xl: 4 };
  return <ui.Stack gap="md">
    <ui.Grid columns={columns} gap={density === "comfortable" ? "lg" : "md"}>
      {rows.map((row) => {
        const key = keyOf(row);
        const statusTone = card.status?.toneKey === undefined ? "neutral" : tone(row[card.status.toneKey]);
        const actions = footerActions.length === 0 ? undefined : <ui.Stack direction="row" gap="xs" wrap>{footerActions.map((action) => <ui.Button key={action.id} kind={action.tone ?? "text"} onClick={() => onRunAction(action, [row])}>{i18n.text(action.label)}</ui.Button>)}</ui.Stack>;
        return <ui.GridItem key={key} span={1}><ui.DataCard
          title={value(row[card.titleKey], { key: card.titleKey }, i18n)}
          subtitle={card.subtitleKey === undefined ? undefined : value(row[card.subtitleKey], { key: card.subtitleKey }, i18n)}
          status={card.status === undefined ? undefined : <ui.Status tone={statusTone}>{value(row[card.status.labelKey], { key: card.status.labelKey }, i18n)}</ui.Status>}
          summary={fields(card.summary, row, i18n, ui)}
          actions={actions}
          selectable={(collection.selection ?? "none") !== "none"}
          selected={selected.has(key)}
          selectionLabel={i18n.text(message(namespace, "selection.card", "选择 {title}", { title: String(row[card.titleKey] ?? key) }))}
          density={density}
          onSelectionChange={(checked) => select(key, checked)}
        >{fields(card.content, row, i18n, ui)}</ui.DataCard></ui.GridItem>;
      })}
    </ui.Grid>
    {nextCursor === undefined ? null : <div ref={sentinel}><ui.Stack direction="row" justify="center">
      {card.loadMore === "viewport" && viewportLoading ? (loadingMore ? <ui.Busy label={i18n.text(message(namespace, "cursor.loading", "正在加载更多"))} /> : null) : <ui.Button kind="secondary" loading={loadingMore} disabled={loadingMore} onClick={onLoadMore}>{i18n.text(message(namespace, "cursor.more", "加载更多"))}</ui.Button>}
    </ui.Stack></div>}
  </ui.Stack>;
}

function fields(specs: readonly CollectionCardFieldSpec[] | undefined, row: CollectionRow, i18n: PortalI18n, ui: ReturnType<typeof usePortalUI>) {
  if (specs === undefined || specs.length === 0) return undefined;
  return <ui.Descriptions columns={1} items={specs.map((field) => ({ id: field.key, label: field.label === undefined ? field.key : i18n.text(field.label), value: value(row[field.key], field, i18n) }))} />;
}

function value(raw: unknown, field: Pick<CollectionCardFieldSpec, "key" | "format">, i18n: PortalI18n): string {
  if (raw === null || raw === undefined) return "—";
  if (field.format === "number" && typeof raw === "number") return i18n.formatNumber(raw);
  if ((field.format === "date" || field.format === "datetime") && (typeof raw === "string" || typeof raw === "number" || raw instanceof Date)) {
    return i18n.formatDate(raw, field.format === "date" ? { dateStyle: "medium" } : { dateStyle: "medium", timeStyle: "short" });
  }
  if (typeof raw === "string" || typeof raw === "number" || typeof raw === "boolean") return String(raw);
  return "—";
}

function tone(raw: unknown): StatusTone {
  return typeof raw === "string" && tones.has(raw as StatusTone) ? raw as StatusTone : "neutral";
}
