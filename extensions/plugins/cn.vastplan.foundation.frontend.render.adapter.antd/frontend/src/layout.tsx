import { Card, Col, Divider as AntdDivider, Flex, Layout, Row, Typography } from "antd";
import { createContext, useContext, type ReactNode } from "react";
import type { BodySectionsProps, DividerProps, FilterBarProps, GridItemProps, GridProps, PageProps, PanelProps, PortalShellProps, ResponsiveColumns, SplitViewProps, StackProps } from "@vastplan/ui-primitives";
import { ComponentSizeProvider, componentSizeRecipes, useComponentSize } from "@vastplan/ui-primitives";
import { gaps } from "./theme";

export function PortalShell({ header, navigation, inspector, statusBar, children, size: requestedSize }: PortalShellProps) {
  const size = useComponentSize(requestedSize);
  return <ComponentSizeProvider size={size}><Layout style={{ minHeight: "100%" }}>
    {header === undefined ? null : <Layout.Header style={{ padding: 0, background: "var(--ant-color-bg-container)" }}>{header}</Layout.Header>}
    <Layout>
      {navigation === undefined ? null : <Layout.Sider width={240} theme="light">{navigation}</Layout.Sider>}
      <Layout.Content>{children}</Layout.Content>
      {inspector === undefined ? null : <Layout.Sider width={320} theme="light">{inspector}</Layout.Sider>}
    </Layout>
    {statusBar === undefined ? null : <Layout.Footer>{statusBar}</Layout.Footer>}
  </Layout></ComponentSizeProvider>;
}

export function Page({ title, actions, children, size: requestedSize }: PageProps) {
  const size = useComponentSize(requestedSize);
  const recipe = componentSizeRecipes.page[size];
  return <ComponentSizeProvider size={size}><main data-size={size} style={{ margin: componentSizeRecipes.layout[size].outerMargin, padding: recipe.padding }}>{title === undefined && actions === undefined ? null : <Flex justify="space-between" align="center" gap={recipe.headerGap} style={{ marginBottom: recipe.headerMargin }}>
    {title === undefined ? <span /> : <Typography.Title level={4} style={{ margin: 0, fontSize: recipe.titleFontSize }}>{title}</Typography.Title>}{actions}
  </Flex>}{children}</main></ComponentSizeProvider>;
}

export function Panel({ title, children, size: requestedSize }: PanelProps) {
  const size = useComponentSize(requestedSize);
  return <ComponentSizeProvider size={size}><Card size={size === "xs" || size === "sm" ? "small" : "medium"} title={title} styles={{ body: { padding: componentSizeRecipes.layout[size].padding } }}>{children}</Card></ComponentSizeProvider>;
}

export function BodySections({ sections, size: requestedSize }: BodySectionsProps) {
  const size = useComponentSize(requestedSize);
  const recipe = componentSizeRecipes.layout[size];
  return <ComponentSizeProvider size={size}><div data-vastplan-body-sections data-size={size} style={{ display: "grid", gap: recipe.sectionGap, width: "100%", minWidth: 0 }}>
    {sections.map((section, index) => <section key={section.id} data-body-section={section.id} style={{ display: "grid", gap: recipe.gap, minWidth: 0, paddingBottom: index === sections.length - 1 ? 0 : recipe.sectionGap, borderBottom: index === sections.length - 1 ? undefined : "1px solid var(--ant-color-border-secondary)" }}>
      {section.title === undefined && section.description === undefined ? null : <div style={{ display: "grid", gap: 4 }}>
        {section.title === undefined ? null : <Typography.Title level={5} style={{ margin: 0, fontSize: 16 }}>{section.title}</Typography.Title>}
        {section.description === undefined ? null : <Typography.Text type="secondary">{section.description}</Typography.Text>}
      </div>}
      {section.content}
    </section>)}
  </div></ComponentSizeProvider>;
}

export function Stack({ direction = "column", gap, align = "stretch", justify = "start", wrap = false, children, size: requestedSize }: StackProps) {
  const size = useComponentSize(requestedSize);
  const justifyContent = justify === "between" ? "space-between" : justify === "start" || justify === "end" ? `flex-${justify}` : justify;
  const alignItems = align === "start" || align === "end" ? `flex-${align}` : align;
  return <ComponentSizeProvider size={size}><div data-size={size} style={{ display: "flex", width: "100%", minWidth: 0, flexDirection: direction, gap: gap === undefined ? componentSizeRecipes.layout[size].gap : gaps[gap], alignItems, justifyContent, flexWrap: wrap ? "wrap" : "nowrap" }}>{children}</div></ComponentSizeProvider>;
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

export function Grid({ columns = 1, gap, children, size: requestedSize }: GridProps) {
  const size = useComponentSize(requestedSize);
  const resolvedGap = gap === undefined ? componentSizeRecipes.layout[size].gap : gaps[gap];
  return <ComponentSizeProvider size={size}><GridColumnsContext.Provider value={columns}><Row data-size={size} gutter={[resolvedGap, resolvedGap]} style={{ width: "100%", minWidth: 0, marginBlock: componentSizeRecipes.layout[size].outerMargin }}>{children}</Row></GridColumnsContext.Provider></ComponentSizeProvider>;
}

export function GridItem({ span = 1, children, size: requestedSize }: GridItemProps) {
  const size = useComponentSize(requestedSize);
  return <ComponentSizeProvider size={size}><Col {...antColumnSpans(useContext(GridColumnsContext), span)} style={{ minWidth: 0 }}>{children}</Col></ComponentSizeProvider>;
}

export function Divider({ label, size: requestedSize }: DividerProps) {
  const size = useComponentSize(requestedSize);
  return <AntdDivider titlePlacement={label === undefined ? undefined : "start"} style={{ marginBlock: componentSizeRecipes.layout[size].gap }}>{label}</AntdDivider>;
}

export function FilterBar({ children, actions, appearance = "default", size: requestedSize }: FilterBarProps) {
  const size = useComponentSize(requestedSize);
  const recipe = componentSizeRecipes.layout[size];
  return appearance === "collection"
    ? <ComponentSizeProvider size={size}><div style={{ width: "100%", minWidth: 0 }}>{children}{actions}</div></ComponentSizeProvider>
    : <ComponentSizeProvider size={size}><Card size={size === "xs" || size === "sm" ? "small" : "medium"} styles={{ body: { padding: recipe.padding } }}><Flex gap={recipe.gap} wrap>{children}</Flex>{actions}</Card></ComponentSizeProvider>;
}

export function SplitView({ primaryLabel, secondaryLabel, primary, secondary, mode = "both", primaryWidth = "md", size: requestedSize }: SplitViewProps) {
  const size = useComponentSize(requestedSize);
  const width = { sm: 280, md: 360, lg: 440 }[primaryWidth];
  const regionStyle = { minWidth: 0, overflow: "auto", border: "1px solid var(--ant-color-border-secondary)", borderRadius: 8, padding: componentSizeRecipes.layout[size].padding, background: "var(--ant-color-bg-container)" } as const;
  return <ComponentSizeProvider size={size}><div style={{ display: "grid", gridTemplateColumns: mode === "both" ? `${width}px minmax(0,1fr)` : "minmax(0,1fr)", gap: componentSizeRecipes.layout[size].gap, width: "100%", minWidth: 0, minHeight: 480 }}>
    {mode === "secondary" ? null : <section aria-label={primaryLabel} style={regionStyle}>{primary}</section>}
    {mode === "primary" ? null : <section aria-label={secondaryLabel} style={regionStyle}>{secondary}</section>}
  </div></ComponentSizeProvider>;
}
