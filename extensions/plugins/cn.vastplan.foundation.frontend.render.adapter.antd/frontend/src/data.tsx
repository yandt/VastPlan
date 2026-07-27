import { Card, Checkbox, Descriptions as AntdDescriptions, Pagination as AntdPagination, Select as AntdSelect, Table as AntdTable, Tag, Typography } from "antd";
import type { ColumnsType } from "antd/es/table";
import type { ReactNode } from "react";
import type { DataCardProps, PaginationProps, ResponsiveColumns, SelectProps, StatusTone, TableProps } from "@vastplan/ui-primitives";

export function Select({ value, options, placeholder, ariaLabel, disabled, onChange }: SelectProps) {
  return <AntdSelect aria-label={ariaLabel} value={value} placeholder={placeholder} disabled={disabled} allowClear style={{ minWidth: 180 }} options={options.map((option) => ({ value: option.value, label: option.label, disabled: option.disabled }))} onChange={(next) => onChange(next)} />;
}

export function Table({ columns, rows, rowKey = "id", selection = "none", selectedRowKeys = [], onSelectionChange, loading, empty, density = "standard", appearance = "default" }: TableProps) {
  const keyOf = (row: Readonly<Record<string, unknown>>) => typeof rowKey === "string" ? String(row[rowKey]) : rowKey(row);
  const antdColumns: ColumnsType<Readonly<Record<string, unknown>>> = columns.map((column) => ({
    key: column.key,
    title: column.title,
    dataIndex: column.key,
    width: column.width,
    fixed: column.fixed,
    align: column.align === "end" ? "right" : column.align === "center" ? "center" : "left",
    render: column.render === undefined ? undefined : (value, row, index) => column.render?.(value, row, index),
  }));
  return <div style={{ width: "100%", overflowX: "auto" }}><AntdTable
    columns={antdColumns}
    dataSource={rows}
    rowKey={keyOf}
    rowSelection={selection === "none" ? undefined : {
      type: selection === "multiple" ? "checkbox" : "radio",
      selectedRowKeys: [...selectedRowKeys],
      onChange: (keys) => onSelectionChange?.(keys.map(String)),
    }}
    loading={loading}
    pagination={false}
    locale={{ emptyText: empty }}
    size={density === "comfortable" ? "large" : density === "compact" ? "small" : "middle"}
    bordered={appearance !== "collection"}
    scroll={{ x: "max-content" }}
  /></div>;
}

export function DataCard({ title, subtitle, status, summary, children, actions, selectable = false, selected = false, selectionLabel, density = "standard", onSelectionChange }: DataCardProps) {
  const padding = density === "compact" ? 12 : density === "comfortable" ? 24 : 16;
  return <Card
    title={<div style={{ minWidth: 0 }}><strong>{title}</strong>{subtitle === undefined ? null : <Typography.Text type="secondary" style={{ display: "block", marginTop: 4 }}>{subtitle}</Typography.Text>}</div>}
    extra={<span style={{ display: "flex", alignItems: "center", gap: 8 }}>{status}{selectable ? <Checkbox aria-label={selectionLabel} checked={selected} onChange={(event) => onSelectionChange?.(event.target.checked)} /> : null}</span>}
    actions={actions === undefined ? undefined : [actions]}
    styles={{ body: { padding } }}
    style={{ height: "100%", borderColor: selected ? "var(--ant-color-primary)" : undefined, boxShadow: selected ? "0 0 0 1px var(--ant-color-primary)" : undefined }}
  >{summary === undefined ? null : <div style={{ marginBottom: density === "comfortable" ? 20 : 12 }}>{summary}</div>}{children}</Card>;
}

export function Pagination({ page, pageSize, pageSizeOptions, total, disabled, align = "start", onChange }: PaginationProps) {
  return <div style={{ display: "flex", justifyContent: align === "end" ? "flex-end" : align === "center" ? "center" : "flex-start" }}><AntdPagination
    current={page}
    pageSize={pageSize}
    pageSizeOptions={pageSizeOptions === undefined ? undefined : [...pageSizeOptions]}
    total={total}
    disabled={disabled}
    showSizeChanger={(pageSizeOptions?.length ?? 0) > 1}
    showTotal={(value) => `共 ${value} 条`}
    onChange={onChange}
  /></div>;
}

export function Descriptions({ title, items, columns = 2 }: { title?: ReactNode; items: Array<{ id: string; label: ReactNode; value: ReactNode }>; columns?: ResponsiveColumns }) {
  const column = typeof columns === "number" ? columns : columns.xs ?? columns.sm ?? columns.md ?? columns.lg ?? columns.xl ?? 2;
  return <AntdDescriptions title={title} bordered column={column} items={items.map((item) => ({ key: item.id, label: item.label, children: item.value }))} />;
}

const statusColors: Record<StatusTone, string | undefined> = { neutral: undefined, info: "blue", success: "green", warning: "orange", error: "red" };
export function Status({ tone = "neutral", children }: { tone?: StatusTone; children: ReactNode }) { return <Tag color={statusColors[tone]}>{children}</Tag>; }
