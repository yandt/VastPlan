import { useRef } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import {
  Box,
  Checkbox,
  CircularProgress,
  Paper,
  Table as MuiTable,
  TableBody,
  TableCell,
  TableHead,
  TableRow,
} from "@mui/material";
import type { TableProps } from "@vastplan/ui-primitives";

export function Table({ columns, rows, rowKey = "id", selection = "none", selectedRowKeys = [], onSelectionChange, loading, empty, density = "standard", virtualization, appearance = "default" }: TableProps) {
  const scrollRef = useRef<HTMLDivElement>(null);
  const keyOf = (row: Readonly<Record<string, unknown>>) => typeof rowKey === "string" ? String(row[rowKey]) : rowKey(row);
  const selected = new Set(selectedRowKeys);
  const virtualized = virtualization?.enabled === true && !loading && rows.length > 0;
  const rowVirtualizer = useVirtualizer({
    count: virtualized ? rows.length : 0,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => virtualization?.rowHeight ?? 48,
    overscan: virtualization?.overscan ?? 4,
    initialRect: { width: 0, height: virtualization?.viewportHeight ?? 0 },
    getItemKey: (index) => keyOf(rows[index]!),
  });
  const virtualRows = virtualized ? rowVirtualizer.getVirtualItems() : [];
  const visibleRows = virtualized
    ? virtualRows.map((virtualRow) => ({ index: virtualRow.index, row: rows[virtualRow.index]! }))
    : rows.map((row, index) => ({ index, row }));
  const topPadding = virtualized && virtualRows.length > 0 ? virtualRows[0]!.start : 0;
  const bottomPadding = virtualized && virtualRows.length > 0 ? rowVirtualizer.getTotalSize() - virtualRows[virtualRows.length - 1]!.end : 0;

  const toggle = (key: string) => {
    if (selection === "single") { onSelectionChange?.(selected.has(key) ? [] : [key]); return; }
    const next = new Set(selected); next.has(key) ? next.delete(key) : next.add(key); onSelectionChange?.([...next]);
  };
  const toggleAll = () => onSelectionChange?.(selected.size === rows.length ? [] : rows.map(keyOf));
  const alignment = (column: TableProps["columns"][number]): "left" | "center" | "right" => column.align === "end" ? "right" : column.align === "center" ? "center" : "left";
  const cellStyle = (column: TableProps["columns"][number], header = false) => ({
    ...(column.width === undefined ? {} : { width: column.width }),
    textAlign: alignment(column),
    ...(header && appearance === "collection" ? { fontWeight: 600 } : {}),
    ...(column.fixed === "right" ? { position: "sticky" as const, right: 0, zIndex: header ? 3 : 1, bgcolor: header ? "action.hover" : "background.paper", boxShadow: "-1px 0 0 rgba(5, 5, 5, 0.06)" } : {}),
  });
  const columnCount = columns.length + (selection === "none" ? 0 : 1);
  const content = <MuiTable stickyHeader={virtualized} aria-rowcount={virtualized ? rows.length + 1 : undefined} size={density === "compact" ? "small" : "medium"}>
    <TableHead sx={appearance === "collection" ? { bgcolor: "action.hover" } : undefined}><TableRow>{selection === "none" ? null : <TableCell padding="checkbox"><Checkbox checked={rows.length > 0 && selected.size === rows.length} indeterminate={selected.size > 0 && selected.size < rows.length} onChange={toggleAll} inputProps={{ "aria-label": "select rows" }} /></TableCell>}{columns.map((column) => <TableCell key={column.key} align={alignment(column)} sx={cellStyle(column, true)}>{column.title}</TableCell>)}</TableRow></TableHead>
    <TableBody>
      {loading ? <TableRow><TableCell colSpan={columnCount}><CircularProgress size={20} /></TableCell></TableRow> : rows.length === 0 ? <TableRow><TableCell colSpan={columnCount}>{empty}</TableCell></TableRow> : <>
        {topPadding === 0 ? null : <TableRow aria-hidden><TableCell colSpan={columnCount} sx={{ height: topPadding, p: 0, border: 0 }} /></TableRow>}
        {visibleRows.map(({ row, index }) => {
          const key = keyOf(row);
          return <TableRow key={key} aria-rowindex={virtualized ? index + 2 : undefined} selected={selected.has(key)} sx={virtualized ? { height: virtualization!.rowHeight } : undefined}>{selection === "none" ? null : <TableCell padding="checkbox"><Checkbox checked={selected.has(key)} onChange={() => toggle(key)} inputProps={{ "aria-label": `select ${key}` }} /></TableCell>}{columns.map((column) => <TableCell key={column.key} align={alignment(column)} sx={cellStyle(column)}>{column.render?.(row[column.key], row, index) ?? String(row[column.key] ?? "")}</TableCell>)}</TableRow>;
        })}
        {bottomPadding === 0 ? null : <TableRow aria-hidden><TableCell colSpan={columnCount} sx={{ height: bottomPadding, p: 0, border: 0 }} /></TableRow>}
      </>}
    </TableBody>
  </MuiTable>;
  const scroll = <Box ref={scrollRef} data-table-scroll={virtualized ? "both" : "horizontal"} data-virtualized={virtualized ? "true" : undefined} sx={{ width: "100%", overflowX: "auto", overflowY: virtualized ? "auto" : "hidden", maxHeight: virtualized ? virtualization!.viewportHeight : undefined }}>{content}</Box>;
  return appearance === "collection" ? scroll : <Paper variant="outlined">{scroll}</Paper>;
}
