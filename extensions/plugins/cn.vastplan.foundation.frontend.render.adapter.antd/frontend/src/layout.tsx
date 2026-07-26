import { Card, Divider as AntdDivider, Flex, Layout, Typography } from "antd";
import type { ReactNode } from "react";
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

function responsiveGrid(columns: ResponsiveColumns): string | Record<string, string> {
  if (typeof columns === "number") return `repeat(${columns}, minmax(0, 1fr))`;
  return Object.fromEntries(Object.entries(columns).map(([key, count]) => [key, `repeat(${count}, minmax(0, 1fr))`]));
}

export function Grid({ columns = 1, gap = "md", children }: GridProps) {
  const templates = responsiveGrid(columns);
  const base = typeof templates === "string" ? templates : templates.xs ?? templates.sm ?? templates.md ?? templates.lg ?? templates.xl ?? "1fr";
  return <div style={{ display: "grid", gridTemplateColumns: base, gap: gaps[gap], width: "100%", minWidth: 0 }}>{children}</div>;
}

export function GridItem({ span = 1, children }: GridItemProps) {
  const value = typeof span === "number" ? span : span.xs ?? span.sm ?? span.md ?? span.lg ?? span.xl ?? 1;
  return <div style={{ gridColumn: `span ${value}`, minWidth: 0 }}>{children}</div>;
}

export function Divider({ label }: { label?: ReactNode }) { return <AntdDivider titlePlacement={label === undefined ? undefined : "start"}>{label}</AntdDivider>; }

export function FilterBar({ children, actions, appearance = "default" }: FilterBarProps) {
  return appearance === "collection"
    ? <div style={{ display: "flex", gap: 24, alignItems: "stretch", flexWrap: "wrap", paddingBottom: 24, borderBottom: "1px solid var(--ant-color-border-secondary)" }}><div style={{ flex: "1 1 720px", minWidth: 0 }}>{children}</div>{actions === undefined ? null : <div style={{ display: "flex", alignItems: "stretch", paddingLeft: 24, borderLeft: "1px solid var(--ant-color-border-secondary)" }}>{actions}</div>}</div>
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
