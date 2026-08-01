import { Card, Checkbox, Descriptions as AntdDescriptions, Pagination as AntdPagination, Select as AntdSelect, Table as AntdTable, Tag, Typography } from "antd";
import type { ColumnsType } from "antd/es/table";
import type { ReactNode } from "react";
import type { DataCardProps, DescriptionsProps, PaginationProps, SelectProps, StatusProps, StatusTone, TableProps } from "@vastplan/ui-primitives";
import { ComponentSizeProvider, componentSizeRecipes, useComponentSize } from "@vastplan/ui-primitives";
import { antdComponentSize } from "./component-size";

export function Select({ value, options, placeholder, ariaLabel, disabled, size: requestedSize, onChange }: SelectProps) {
  const size = useComponentSize(requestedSize);
  const recipe = componentSizeRecipes.control[size];
  return <AntdSelect aria-label={ariaLabel} value={value} placeholder={placeholder} disabled={disabled} size={antdComponentSize[size]} allowClear style={{ minWidth: 180, height: recipe.height, fontSize: recipe.fontSize }} options={options.map((option) => ({ value: option.value, label: option.label, disabled: option.disabled }))} onChange={(next) => onChange(next)} />;
}

export function Table({ columns, rows, rowKey = "id", selection = "none", selectedRowKeys = [], onSelectionChange, loading, empty, density = "standard", virtualization, appearance = "default", size: requestedSize }: TableProps) {
  const size = useComponentSize(requestedSize);
  const keyOf = (row: Readonly<Record<string, unknown>>) => typeof rowKey === "string" ? String(row[rowKey]) : rowKey(row);
  const virtualized = virtualization?.enabled === true && !loading && rows.length > 0;
  const scrollWidth = columns.reduce((width, column) => width + (column.width ?? 160), selection === "none" ? 0 : 48);
  const antdColumns: ColumnsType<Readonly<Record<string, unknown>>> = columns.map((column) => ({
    key: column.key,
    title: column.title,
    dataIndex: column.key,
    width: column.width,
    fixed: column.fixed,
    align: column.align === "end" ? "right" : column.align === "center" ? "center" : "left",
    render: column.render === undefined ? undefined : (value, row, index) => column.render?.(value, row, index),
  }));
  return <ComponentSizeProvider size={size}><div data-table-scroll="horizontal" data-size={size} style={{ width: "100%", overflowX: "auto", overflowY: "hidden" }}><AntdTable
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
    virtual={virtualized}
    scroll={{ x: virtualized ? scrollWidth : "max-content", y: virtualized ? virtualization!.viewportHeight : undefined }}
  /></div></ComponentSizeProvider>;
}

export function DataCard({ title, subtitle, status, summary, children, actions, selectable = false, selected = false, selectionLabel, density = "standard", onSelectionChange, size: requestedSize }: DataCardProps) {
  const size = useComponentSize(requestedSize);
  const padding = density === "compact" ? 12 : density === "comfortable" ? 24 : 16;
  return <ComponentSizeProvider size={size}><Card
    size={size === "xs" || size === "sm" ? "small" : "medium"}
    title={<div style={{ minWidth: 0 }}><strong>{title}</strong>{subtitle === undefined ? null : <Typography.Text type="secondary" style={{ display: "block", marginTop: 4 }}>{subtitle}</Typography.Text>}</div>}
    extra={<span style={{ display: "flex", alignItems: "center", gap: 8 }}>{status}{selectable ? <Checkbox aria-label={selectionLabel} checked={selected} onChange={(event) => onSelectionChange?.(event.target.checked)} /> : null}</span>}
    actions={actions === undefined ? undefined : [actions]}
    styles={{ body: { padding } }}
    style={{ height: "100%", borderColor: selected ? "var(--ant-color-primary)" : undefined, boxShadow: selected ? "0 0 0 1px var(--ant-color-primary)" : undefined }}
  >{summary === undefined ? null : <div style={{ marginBottom: density === "comfortable" ? 20 : 12 }}>{summary}</div>}{children}</Card></ComponentSizeProvider>;
}

export function Pagination({ page, pageSize, pageSizeOptions, total, disabled, align = "start", size: requestedSize, onChange }: PaginationProps) {
  const size = useComponentSize(requestedSize);
  const recipe = componentSizeRecipes.pagination[size];
  return <div style={{ display: "flex", justifyContent: align === "end" ? "flex-end" : align === "center" ? "center" : "flex-start" }}><AntdPagination
    current={page}
    pageSize={pageSize}
    pageSizeOptions={pageSizeOptions === undefined ? undefined : [...pageSizeOptions]}
    total={total}
    disabled={disabled}
    size={size === "xs" || size === "sm" ? "small" : undefined}
    style={{ ["--ant-pagination-item-size" as string]: `${recipe.itemEdge}px`, fontSize: recipe.fontSize }}
    showSizeChanger={(pageSizeOptions?.length ?? 0) > 1}
    showTotal={(value) => `共 ${value} 条`}
    onChange={onChange}
  /></div>;
}

export function Descriptions({ title, items, columns = 2, size: requestedSize }: DescriptionsProps) {
  const size = useComponentSize(requestedSize);
  const recipe = componentSizeRecipes.descriptions[size];
  return <ComponentSizeProvider size={size}><AntdDescriptions
    data-size={size}
    title={title === undefined ? undefined : <span style={{ fontSize: recipe.titleFontSize }}>{title}</span>}
    bordered
    size={size === "xs" || size === "sm" ? "small" : "middle"}
    column={columns}
    styles={{ label: { paddingBlock: recipe.cellPaddingBlock, paddingInline: recipe.cellPaddingInline, fontSize: recipe.fontSize }, content: { paddingBlock: recipe.cellPaddingBlock, paddingInline: recipe.cellPaddingInline, fontSize: recipe.fontSize } }}
    items={items.map((item) => ({ key: item.id, label: item.label, children: item.value, span: item.span }))}
  /></ComponentSizeProvider>;
}

const statusColors: Record<StatusTone, string | undefined> = { neutral: undefined, info: "blue", success: "green", warning: "orange", error: "red" };
export function Status({ tone = "neutral", children, size: requestedSize }: StatusProps) {
  const size = useComponentSize(requestedSize);
  return <Tag color={statusColors[tone]} style={{ marginInlineEnd: 0, fontSize: componentSizeRecipes.control[size].fontSize, lineHeight: `${componentSizeRecipes.control[size].height - 4}px` }}>{children}</Tag>;
}
