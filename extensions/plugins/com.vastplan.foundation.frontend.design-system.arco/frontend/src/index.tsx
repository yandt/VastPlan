import {
  Alert,
  Breadcrumb as ArcoBreadcrumb,
  Button,
  Card,
  ConfigProvider,
  Descriptions as ArcoDescriptions,
  Divider as ArcoDivider,
  Drawer as ArcoDrawer,
  Empty,
  Grid as ArcoGrid,
  Input,
  Layout,
  Menu as ArcoMenu,
  Modal,
  Notification,
  Pagination as ArcoPagination,
  Skeleton as ArcoSkeleton,
  Space,
  Spin,
  Table as ArcoTable,
  Tabs as ArcoTabs,
  Tag,
  Typography,
  IconCheckCircle,
  IconClose,
  IconCloseCircle,
  IconDelete,
  IconEdit,
  IconExclamationCircle,
  IconInfoCircle,
  IconMenu,
  IconPlus,
  IconSearch,
  IconSettings,
} from "./arco-components";
import { useMemo, useRef } from "react";
import type { ComponentType, ReactNode } from "react";
import type {
  ButtonProps,
  CommandItem,
  DesignSystemAdapter,
  DialogProps,
  DrawerProps,
  GridItemProps,
  GridProps,
  MenuItem,
  PortalShellProps,
  PortalUI,
  ResponsiveColumns,
  SemanticIconName,
  StackProps,
  StatusTone,
  TableProps,
} from "@vastplan/portal-ui";
import { PortalUIProvider } from "@vastplan/portal-ui";
import { arcoCSS } from "./arco-styles";
import { ArcoJSONSchemaForm } from "./json-schema-form";
import { scopeDocumentCSS } from "./scope-css";

const gapPixels = { xs: 4, sm: 8, md: 16, lg: 24 } as const;
const dialogWidths = { sm: 480, md: 720, lg: 960 } as const;
const iconSizes = { sm: 14, md: 16, lg: 20 } as const;

function PortalShell({ header, navigation, inspector, statusBar, children }: PortalShellProps) {
  return <Layout style={{ minHeight: "100%" }}>
    {header === undefined ? null : <Layout.Header>{header}</Layout.Header>}
    <Layout>
      {navigation === undefined ? null : <Layout.Sider width={240}>{navigation}</Layout.Sider>}
      <Layout.Content>{children}</Layout.Content>
      {inspector === undefined ? null : <Layout.Sider width={320}>{inspector}</Layout.Sider>}
    </Layout>
    {statusBar === undefined ? null : <Layout.Footer>{statusBar}</Layout.Footer>}
  </Layout>;
}

function Page({ title, actions, children }: { title?: string; actions?: ReactNode; children: ReactNode }) {
  return <main style={{ padding: 24 }}>
    {title === undefined && actions === undefined ? null : <header style={{ display: "flex", justifyContent: "space-between", alignItems: "center", gap: 16 }}>
      {title === undefined ? null : <Typography.Title heading={4}>{title}</Typography.Title>}
      {actions}
    </header>}
    {children}
  </main>;
}

function Panel({ title, children }: { title?: string; children: ReactNode }) {
  return <Card title={title}>{children}</Card>;
}

function Stack({ direction = "column", gap = "md", align = "stretch", justify = "start", wrap = false, children }: StackProps) {
  return <Space
    direction={direction === "column" ? "vertical" : "horizontal"}
    size={gapPixels[gap]}
    align={align === "stretch" ? "start" : align}
    wrap={wrap}
    style={{ width: direction === "column" || align === "stretch" ? "100%" : undefined, justifyContent: justify === "between" ? "space-between" : justify }}
  >{children}</Space>;
}

function Grid({ columns = 1, gap = "md", children }: GridProps) {
  return <ArcoGrid cols={cascadeResponsiveColumns(columns)} rowGap={gapPixels[gap]} colGap={gapPixels[gap]}>{children}</ArcoGrid>;
}

const responsiveBreakpoints = ["xs", "sm", "md", "lg", "xl"] as const;

/** Arco does not cascade a lower breakpoint value when the current breakpoint key is absent. */
export function cascadeResponsiveColumns(columns: ResponsiveColumns): ResponsiveColumns {
  if (typeof columns === "number") return columns;
  const cascaded: Exclude<ResponsiveColumns, number> = {};
  let inherited: number | undefined;
  for (const breakpoint of responsiveBreakpoints) {
    inherited = columns[breakpoint] ?? inherited;
    if (inherited !== undefined) cascaded[breakpoint] = inherited;
  }
  return cascaded;
}

// Arco Grid only retains direct children carrying its private GridItem marker.
// A wrapper component looks equivalent in JSX but is silently filtered out,
// so expose the native item through the framework-neutral contract.
const GridItem = ArcoGrid.GridItem as unknown as ComponentType<GridItemProps>;

function renderMenuItems(items: MenuItem[], onSelect?: (id: string) => void, parentDisabled = false): ReactNode[] {
  return items.map((item) => item.children?.length
    ? <ArcoMenu.SubMenu key={item.id} title={item.label}>{renderMenuItems(item.children, onSelect, parentDisabled || item.disabled === true)}</ArcoMenu.SubMenu>
    : <ArcoMenu.Item key={item.id} disabled={parentDisabled || item.disabled}>{item.icon}{item.href === undefined ? item.label : <a href={item.href} onClick={(event) => {
      event.preventDefault();
      event.stopPropagation();
      if (!parentDisabled && item.disabled !== true) onSelect?.(item.id);
    }}>{item.label}</a>}</ArcoMenu.Item>);
}

function Menu({ items, activeID, onSelect }: { items: MenuItem[]; activeID?: string; onSelect?(id: string): void }) {
  return <ArcoMenu selectedKeys={activeID ? [activeID] : []} onClickMenuItem={(key) => onSelect?.(key)}>{renderMenuItems(items, onSelect)}</ArcoMenu>;
}

function CommandPalette({ open, commands, query, onQueryChange, onClose }: { open: boolean; commands: CommandItem[]; query: string; onQueryChange(query: string): void; onClose(): void }) {
  const term = query.trim().toLocaleLowerCase();
  const visible = term === "" ? commands : commands.filter((command) => [command.title, command.description, ...(command.keywords ?? [])].some((part) => part?.toLocaleLowerCase().includes(term)));
  return <Modal visible={open} title="命令" footer={null} onCancel={onClose} unmountOnExit>
    <Space direction="vertical" size={12} style={{ width: "100%" }}>
      <Input autoFocus value={query} placeholder="搜索命令" onChange={onQueryChange} />
      {visible.length === 0 ? <Empty description="没有匹配命令" /> : visible.map((command) => <Button
        key={command.id}
        long
        disabled={command.disabled}
        onClick={() => { command.run(); onClose(); }}
      >{command.title}{command.description === undefined ? null : ` — ${command.description}`}</Button>)}
    </Space>
  </Modal>;
}

function Dialog({ open, title, children, footer, width = "md", onClose }: DialogProps) {
  return <Modal visible={open} title={title} footer={footer ?? null} style={{ width: dialogWidths[width] }} onCancel={onClose} unmountOnExit>{children}</Modal>;
}

function Drawer({ open, title, children, footer, width = "md", placement = "right", onClose }: DrawerProps) {
  const size = dialogWidths[width];
  return <ArcoDrawer
    visible={open}
    title={title}
    footer={footer ?? null}
    placement={placement}
    width={placement === "left" || placement === "right" ? size : undefined}
    height={placement === "top" || placement === "bottom" ? size : undefined}
    onCancel={onClose}
    unmountOnExit
  >{children}</ArcoDrawer>;
}

function buttonProps({ kind }: Pick<ButtonProps, "kind">): { type?: "primary" | "secondary" | "text"; status?: "danger" } {
  if (kind === "primary") return { type: "primary" };
  if (kind === "danger") return { status: "danger" };
  if (kind === "text") return { type: "text" };
  return { type: "secondary" };
}

function Table({ columns, rows, rowKey = "id", loading, empty }: TableProps) {
  return <ArcoTable
    columns={columns.map((column) => ({
      title: column.title,
      dataIndex: column.key,
      width: column.width,
      render: column.render === undefined ? undefined : (cell: unknown, row: Readonly<Record<string, unknown>>, index: number) => column.render?.(cell, row, index),
    }))}
    data={rows as Array<Record<string, unknown>>}
    rowKey={typeof rowKey === "string" ? rowKey : (row: Record<string, unknown>) => rowKey(row)}
    loading={loading}
    pagination={false}
    noDataElement={empty}
    scroll={{ x: "max-content" }}
  />;
}

function columnsForDescriptions(columns: ResponsiveColumns | undefined): number | Record<string, number> | undefined {
  return columns;
}

function SemanticIcon({ name, label, size = "md" }: { name: SemanticIconName; label?: string; size?: "sm" | "md" | "lg" }) {
  const props = { style: { fontSize: iconSizes[size] }, ...(label === undefined ? { "aria-hidden": true } : { "aria-label": label }) };
  switch (name) {
    case "add": return <IconPlus {...props} />;
    case "remove": return <IconDelete {...props} />;
    case "edit": return <IconEdit {...props} />;
    case "search": return <IconSearch {...props} />;
    case "settings": return <IconSettings {...props} />;
    case "success": return <IconCheckCircle {...props} />;
    case "warning": return <IconExclamationCircle {...props} />;
    case "error": return <IconCloseCircle {...props} />;
    case "info": return <IconInfoCircle {...props} />;
    case "close": return <IconClose {...props} />;
    case "menu": return <IconMenu {...props} />;
  }
}

const statusColors: Record<StatusTone, string | undefined> = {
  neutral: undefined,
  info: "arcoblue",
  success: "green",
  warning: "orange",
  error: "red",
};

type ArcoComponents = Omit<PortalUI, "notify" | "confirm">;

export const arcoPortalUIComponents: ArcoComponents = {
  PortalShell,
  Page,
  Panel,
  Stack,
  Grid,
  GridItem,
  Divider: ({ label }) => <ArcoDivider orientation={label === undefined ? undefined : "left"}>{label}</ArcoDivider>,
  Button: ({ children, kind, ...props }) => <Button {...buttonProps({ kind })} {...props}>{children}</Button>,
  Menu,
  Breadcrumb: ({ items }) => <ArcoBreadcrumb>{items.map((item) => <ArcoBreadcrumb.Item key={item.id} href={item.href} onClick={item.onSelect}>{item.label}</ArcoBreadcrumb.Item>)}</ArcoBreadcrumb>,
  Tabs: ({ items, activeID, onChange }) => <ArcoTabs activeTab={activeID} onChange={onChange}>{items.map((item) => <ArcoTabs.TabPane key={item.id} title={item.label} disabled={item.disabled}>{item.content}</ArcoTabs.TabPane>)}</ArcoTabs>,
  CommandPalette,
  Dialog,
  Drawer,
  FormRenderer: ArcoJSONSchemaForm,
  FilterBar: ({ children, actions }) => <Card size="small"><Space wrap size={12}>{children}</Space>{actions === undefined ? null : <div style={{ float: "right" }}>{actions}</div>}</Card>,
  Table,
  Pagination: ({ page, pageSize, total, disabled, onChange }) => <ArcoPagination current={page} pageSize={pageSize} total={total} disabled={disabled} showTotal sizeCanChange onChange={onChange} />,
  Descriptions: ({ title, items, columns }) => <ArcoDescriptions title={title} data={items.map((item) => ({ key: item.id, label: item.label, value: item.value }))} column={columnsForDescriptions(columns)} border />,
  Status: ({ tone = "neutral", children }) => <Tag color={statusColors[tone]}>{children}</Tag>,
  Icon: SemanticIcon,
  theme: {
    mode: "system",
    tokens: {
      color: {
        canvas: "var(--color-bg-1)", surface: "var(--color-bg-2)", text: "var(--color-text-1)",
        mutedText: "var(--color-text-3)", border: "var(--color-border-2)", primary: "rgb(var(--primary-6))", danger: "rgb(var(--danger-6))",
      },
      radius: { sm: 2, md: 4, lg: 8 },
      spacing: gapPixels,
    },
  },
  EmptyState: ({ title, description }) => <Empty description={<><strong>{title}</strong>{description === undefined ? null : <div>{description}</div>}</>} />,
  ErrorState: ({ title, retry }) => <Alert type="error" title={title} action={retry ? <Button onClick={retry}>重试</Button> : undefined} />,
  Skeleton: ({ rows = 3 }) => <ArcoSkeleton animation text={{ rows }} />,
  Busy: ({ label }) => <Spin tip={label} />,
};

// Ordinary Arco selectors are already isolated by the Portal shadow tree.
// Translate document-root selectors so tokens and normalization apply to that
// shadow host instead of leaking into the surrounding page.
export const scopedArcoCSS = scopeDocumentCSS(arcoCSS);

function ArcoProvider({ children }: { children: ReactNode }) {
  const popupRoot = useRef<HTMLDivElement>(null);
  const requirePopupRoot = () => {
    if (popupRoot.current === null) throw new Error("Arco overlay root 尚未挂载");
    return popupRoot.current;
  };
  const [notifications, notificationHolder] = Notification.useNotification({ getContainer: requirePopupRoot });
  const [modals, modalHolder] = Modal.useModal();
  const ui = useMemo<PortalUI>(() => ({
    ...arcoPortalUIComponents,
    notify: ({ title, content, kind = "info" }) => notifications[kind]?.({ title, content: content ?? "" }),
    confirm: ({ title, content }) => new Promise((resolve) => {
      if (modals.confirm === undefined) { resolve(false); return; }
      modals.confirm({ title, content, onOk: () => resolve(true), onCancel: () => resolve(false) });
    }),
  }), [modals, notifications]);

  return <>
    <style data-vastplan-design-system="arco">{scopedArcoCSS}</style>
    <div ref={popupRoot} data-vastplan-design-system="arco">
      <ConfigProvider getPopupContainer={requirePopupRoot}>
        <PortalUIProvider ui={ui}>{children}</PortalUIProvider>
        {notificationHolder}
        {modalHolder}
      </ConfigProvider>
    </div>
  </>;
}

export const arcoDesignSystem: DesignSystemAdapter = {
  id: "ui.design-system",
  framework: "arco",
  uiContract: "1.0.0",
  capabilities: ["layout", "menu", "overlay", "form", "data", "feedback", "theme", "navigation"],
  Provider: ArcoProvider,
};

export default arcoDesignSystem;
