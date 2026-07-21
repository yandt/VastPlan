import { useCallback, useEffect, useRef, useState } from "react";
import type { CollectionPageDefinition, CollectionQuery } from "@vastplan/workbench-sdk";
import type { CollectionRow } from "./model.js";

export interface CollectionDataState {
  rows: readonly CollectionRow[];
  total: number;
  nextCursor?: string;
  loading: boolean;
  refreshing: boolean;
  loadingMore: boolean;
  failure?: string;
  resetToken: number;
  refresh(): void;
  loadMore(): void;
}

export function mergeCursorRows(previous: readonly CollectionRow[], incoming: readonly CollectionRow[], keyOf: (row: CollectionRow) => string): readonly CollectionRow[] {
  const merged = [...previous];
  const indexes = new Map(merged.map((row, index) => [keyOf(row), index]));
  for (const row of incoming) {
    const key = keyOf(row);
    const index = indexes.get(key);
    if (index === undefined) {
      indexes.set(key, merged.length);
      merged.push(row);
    } else {
      merged[index] = row;
    }
  }
  return merged;
}

export function normalizeNextCursor(requested: string | undefined, returned: string | undefined): string | undefined {
  const next = returned === "" ? undefined : returned;
  if (next !== undefined && next === requested) throw new Error("Cursor loader 返回了与请求相同的 nextCursor");
  return next;
}

export function useCollectionData({ page, pageNumber, pageSize, filters, keyOf }: {
  page: CollectionPageDefinition;
  pageNumber: number;
  pageSize: number;
  filters: Readonly<Record<string, unknown>>;
  keyOf(row: CollectionRow): string;
}): CollectionDataState {
  const [rows, setRows] = useState<readonly CollectionRow[]>([]);
  const [total, setTotal] = useState(0);
  const [nextCursor, setNextCursor] = useState<string>();
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [loadingMore, setLoadingMore] = useState(false);
  const [failure, setFailure] = useState<string>();
  const [resetToken, setResetToken] = useState(0);
  const requestRef = useRef<AbortController>();
  const rowsRef = useRef<readonly CollectionRow[]>([]);
  const cursorRef = useRef<string>();

  useEffect(() => { rowsRef.current = rows; }, [rows]);
  useEffect(() => { cursorRef.current = nextCursor; }, [nextCursor]);

  const request = useCallback((intent: "replace" | "refresh" | "append") => {
    if (intent === "append" && cursorRef.current === undefined) return;
    requestRef.current?.abort();
    const controller = new AbortController();
    requestRef.current = controller;
    const currentRows = rowsRef.current;
    if (intent === "append") setLoadingMore(true);
    else if (currentRows.length > 0) setRefreshing(true);
    else setLoading(true);
    const query: CollectionQuery = {
      mode: page.collection.query.mode,
      page: pageNumber,
      pageSize,
      filters,
      ...(page.collection.query.mode === "cursor" && intent === "append" ? { cursor: cursorRef.current } : {}),
    };
    void page.load(query, controller.signal).then((result) => {
      if (controller.signal.aborted) return;
      const incoming = result.items as readonly CollectionRow[];
      const cursor = query.mode === "cursor" ? normalizeNextCursor(query.cursor, result.nextCursor) : undefined;
      const nextRows = query.mode === "cursor" && intent === "append" ? mergeCursorRows(currentRows, incoming, keyOf) : incoming;
      rowsRef.current = nextRows;
      setRows(nextRows);
      setTotal(result.total ?? nextRows.length);
      cursorRef.current = cursor;
      setNextCursor(cursor);
      setFailure(undefined);
      if (intent !== "append") setResetToken((value) => value + 1);
    }).catch((error: unknown) => {
      if (!controller.signal.aborted) setFailure(error instanceof Error ? error.message : String(error));
    }).finally(() => {
      if (!controller.signal.aborted) { setLoading(false); setRefreshing(false); setLoadingMore(false); }
    });
  }, [filters, keyOf, page, pageNumber, pageSize]);

  useEffect(() => { request("replace"); return () => requestRef.current?.abort(); }, [request]);
  const refresh = useCallback(() => request("refresh"), [request]);
  const loadMore = useCallback(() => request("append"), [request]);
  return {
    rows, total, nextCursor, loading, refreshing, loadingMore, failure, resetToken,
    refresh,
    loadMore,
  };
}
