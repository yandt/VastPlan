import { Card, Col, Divider as AntdDivider, Flex, Layout, Row, Typography } from "antd";
import { createContext, useContext, type ReactNode } from "react";
import type { FilterBarProps, GridItemProps, GridProps, PageProps, PanelProps, PortalShellProps, ResponsiveColumns, SplitViewProps, StackProps } from "@vastplan/ui-primitives";
import { gaps } from "./theme";

export function PortalShell({ header, navigation, inspector, statusBar, children }: PortalShellProps) {
  return <Layout style={{ minHeight: "100%" }}>
    {header === undefined ? null : <Layout.Header style={{ padding: 0, background: "var(--ant-color-bg-container)" }}>{header}</Layout.Header>}
    <Layout>
      {navigation === undefined ? null : <Layout.Sider width={240} theme="light">{navigation}</Layout.Sider>}
      <Layout.Content>{children}</Layout.Content>
      {inspector === undefined ? null : <Layout.Sider width={320} theme="light">{inspector}</Layout.Sider>}
    </Layout>
    {statusBar === undefined ? null : <Layout.Footer>{statusBar}</Layout.Footer>}
  </Layout>;
}

export function Page({ title, actions, children }: PageProps) {
  return <main style={{ padding: 24 }}>{title === undefined && actions === undefined ? null : <Flex justify="space-between" align="center" gap={16} style={{ marginBottom: 16 }}>
    {title === undefined ? <span /> : <Typography.Title level={4} style={{ margin: 0 }}>{title}</Typography.Title>}{actions}
  </Flex>}{children}</main>;
}

export function Panel({ title, children }: PanelProps) { return <Card title={title}>{children}</Card>; }

export function Stack({ direction = "column", gap = "md", align = "stretch", justify = "start", wrap = false, children }: StackProps) {
  const justifyContent = justify === "between" ? "space-between" : justify === "start" || justify === "end" ? `flex-${justify}` : justify;
  const alignItems = align === "start" || align === "end" ? `flex-${align}` : align;
  return <div style={{ display: "flex", width: "100%", minWidth: 0, flexDirection: direction, gap: gaps[gap], alignItems, justifyContent, flexWrap: wrap ? "wrap" : "nowrap" }}>{children}</div>;
}

const breakpoints = ["xs", "sm", "md", "lg", "xl"] as const;
type Breakpoint = typeof breakpoints[number];
const GridColumnsContext = createContext<ResponsiveColumns>(1);

function cascadeColumns(columns: ResponsiveColumns): Record<Breakpoint, number> {
  const source = typeof columns === "number" ? { xs: columns } : columns;
  let inherited = 1;
  return Object.fromEntries(breakpoints.map((breakpoint) => {
    inherited = source[breakpoint] ?? inherited;
    return [breakpoint, Math.max(1, inherited)];
  })) as Record<Breakpoint, number>;
}

function antColumnSpans(columns: ResponsiveColumns, span: ResponsiveColumns): Record<Breakpoint, { span: number }> {
  const grid = cascadeColumns(columns);
  const requested = cascadeColumns(span);
  return Object.fromEntries(breakpoints.map((breakpoint) => [breakpoint, { span: Math.min(24, Math.max(1, Math.round(24 * requested[breakpoint] / grid[breakpoint]))) }])) as Record<Breakpoint, { span: number }>;
}

export function Grid({ columns = 1, gap = "md", children }: GridProps) {
  return <GridColumnsContext.Provider value={columns}><Row gutter={[gaps[gap], gaps[gap]]} style={{ width: "100%", minWidth: 0 }}>{children}</Row></GridColumnsContext.Provider>;
}

export function GridItem({ span = 1, children }: GridItemProps) {
  return <Col {...antColumnSpans(useContext(GridColumnsContext), span)} style={{ minWidth: 0 }}>{children}</Col>;
}

export function Divider({ label }: { label?: ReactNode }) { return <AntdDivider titlePlacement={label === undefined ? undefined : "start"}>{label}</AntdDivider>; }

export function FilterBar({ children, actions, appearance = "default" }: FilterBarProps) {
  return appearance === "collection"
    ? <div style={{ width: "100%", minWidth: 0 }}>{children}{actions}</div>
    : <Card size="small"><Flex gap={12} wrap>{children}</Flex>{actions}</Card>;
}

export function SplitView({ primaryLabel, secondaryLabel, primary, secondary, mode = "both", primaryWidth = "md" }: SplitViewProps) {
  const width = { sm: 280, md: 360, lg: 440 }[primaryWidth];
  return <div style={{ display: "grid", gridTemplateColumns: mode === "both" ? `${width}px minmax(0,1fr)` : "minmax(0,1fr)", gap: 16, width: "100%", minWidth: 0, minHeight: 480 }}>
    {mode === "secondary" ? null : <section aria-label={primaryLabel} style={regionStyle}>{primary}</section>}
    {mode === "primary" ? null : <section aria-label={secondaryLabel} style={regionStyle}>{secondary}</section>}
  </div>;
}

const regionStyle: React.CSSProperties = { minWidth: 0, overflow: "auto", border: "1px solid var(--ant-color-border-secondary)", borderRadius: 8, padding: 16, background: "var(--ant-color-bg-container)" };
