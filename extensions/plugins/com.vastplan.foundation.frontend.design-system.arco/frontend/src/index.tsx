import {
  Alert,
  Breadcrumb as ArcoBreadcrumb,
  Button,
  Card,
  ConfigProvider,
  DatePicker,
  Descriptions as ArcoDescriptions,
  Divider as ArcoDivider,
  Drawer as ArcoDrawer,
  Form,
  Grid as ArcoGrid,
  Input,
  InputNumber,
  Layout,
  Menu as ArcoMenu,
  Modal,
  Notification,
  Pagination as ArcoPagination,
  Result,
  Select,
  Skeleton as ArcoSkeleton,
  Space,
  Spin,
  Switch,
  Table as ArcoTable,
  Tabs as ArcoTabs,
  Tag,
  Typography,
} from "@arco-design/web-react";
import {
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
} from "@arco-design/web-react/icon";
import arcoCSS from "@arco-design/web-react/dist/css/arco.css";
import { useEffect, useMemo, useRef, useState } from "react";
import type { ReactNode } from "react";
import type {
  ButtonProps,
  CommandItem,
  DesignSystemAdapter,
  DialogProps,
  DrawerProps,
  FormField,
  FormRendererProps,
  FormValidationIssue,
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
import {
  applyFormDefaults,
  getFormValue,
  isFormFieldVisible,
  PortalUIProvider,
  validateForm,
} from "@vastplan/portal-ui";
import { scopeDocumentCSS } from "./scope-css";

const gapPixels = { xs: 4, sm: 8, md: 16, lg: 24 } as const;
const dialogWidths = { sm: 480, md: 720, lg: 960 } as const;
const emptyFormContext: Readonly<Record<string, unknown>> = {};
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
  return <ArcoGrid cols={columns} rowGap={gapPixels[gap]} colGap={gapPixels[gap]}>{children}</ArcoGrid>;
}

function GridItem({ span = 1, children }: GridItemProps) {
  return <ArcoGrid.GridItem span={span}>{children}</ArcoGrid.GridItem>;
}

function renderMenuItems(items: MenuItem[], parentDisabled = false): ReactNode[] {
  return items.map((item) => item.children?.length
    ? <ArcoMenu.SubMenu key={item.id} title={item.label}>{renderMenuItems(item.children, parentDisabled || item.disabled === true)}</ArcoMenu.SubMenu>
    : <ArcoMenu.Item key={item.id} disabled={parentDisabled || item.disabled}>{item.icon}{item.label}</ArcoMenu.Item>);
}

function Menu({ items, activeID, onSelect }: { items: MenuItem[]; activeID?: string; onSelect?(id: string): void }) {
  return <ArcoMenu selectedKeys={activeID ? [activeID] : []} onClickMenuItem={(key) => onSelect?.(key)}>{renderMenuItems(items)}</ArcoMenu>;
}

function CommandPalette({ open, commands, query, onQueryChange, onClose }: { open: boolean; commands: CommandItem[]; query: string; onQueryChange(query: string): void; onClose(): void }) {
  const term = query.trim().toLocaleLowerCase();
  const visible = term === "" ? commands : commands.filter((command) => [command.title, command.description, ...(command.keywords ?? [])].some((part) => part?.toLocaleLowerCase().includes(term)));
  return <Modal visible={open} title="命令" footer={null} onCancel={onClose} unmountOnExit>
    <Space direction="vertical" size={12} style={{ width: "100%" }}>
      <Input autoFocus value={query} placeholder="搜索命令" onChange={onQueryChange} />
      {visible.length === 0 ? <Result status="404" title="没有匹配命令" /> : visible.map((command) => <Button
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

type Path = Array<string | number>;

function pathString(path: Path): string {
  return path.reduce<string>((result, part) => typeof part === "number" ? `${result}[${part}]` : result === "" ? part : `${result}.${part}`, "");
}

function replaceAtPath(root: Record<string, unknown>, path: Path, nextValue: unknown): Record<string, unknown> {
  const [head, ...tail] = path;
  if (head === undefined) return root;
  const source = root[head] as unknown;
  if (tail.length === 0) return { ...root, [head]: nextValue };
  if (Array.isArray(source)) {
    const next = [...source];
    const index = tail[0];
    if (typeof index !== "number") return root;
    if (tail.length === 1) next[index] = nextValue;
    else next[index] = replaceAtPath((next[index] as Record<string, unknown> | undefined) ?? {}, tail.slice(1), nextValue);
    return { ...root, [head]: next };
  }
  return { ...root, [head]: replaceAtPath((source as Record<string, unknown> | undefined) ?? {}, tail, nextValue) };
}

function defaultArrayEntry(field: FormField): unknown {
  if (field.fields === undefined) return "";
  return applyFormDefaults({ id: `${field.key}.item`, fields: field.fields }, {});
}

function issueText(issue: FormValidationIssue): string {
  if (issue.message !== undefined) return issue.message;
  switch (issue.code) {
    case "required": return "此项为必填项";
    case "min": return `不能小于 ${issue.limit ?? "最小值"}`;
    case "max": return `不能大于 ${issue.limit ?? "最大值"}`;
    case "pattern": return "格式不符合要求";
    case "invalidPattern": return "表单校验规则无效";
  }
}

function optionToken(field: FormField, value: unknown): string | undefined {
  const index = field.options?.findIndex((option) => Object.is(option.value, value)) ?? -1;
  return index < 0 ? undefined : `option:${index}`;
}

function optionValue(field: FormField, token: string): unknown {
  const index = Number(token.slice("option:".length));
  return field.options?.[index]?.value;
}

function selectOptions(field: FormField) {
  return (field.options ?? []).map((option, index) => ({ label: option.label, value: `option:${index}`, disabled: option.disabled }));
}

function FormRenderer({ schema, value, onChange, readOnly, submitting, errors: externalErrors, context: suppliedContext, validate, validationDelayMs = 250, onValidationChange }: FormRendererProps) {
  const context = suppliedContext ?? emptyFormContext;
  const effectiveValue = useMemo(() => applyFormDefaults(schema, value), [schema, value]);
  const validation = useMemo(() => validateForm(schema, effectiveValue), [schema, effectiveValue]);
  useEffect(() => {
    if (effectiveValue !== value) onChange(effectiveValue);
  }, [effectiveValue, onChange, value]);
  const [asyncValidation, setAsyncValidation] = useState<{
    source?: Readonly<Record<string, unknown>>;
    validating: boolean;
    errors: Readonly<Record<string, string>>;
  }>({ validating: false, errors: {} });
  const currentAsyncValidation = asyncValidation.source === effectiveValue
    ? asyncValidation
    : { source: effectiveValue, validating: validate !== undefined && validation.valid, errors: {} };

  useEffect(() => {
    if (validate === undefined || !validation.valid) {
      setAsyncValidation({ source: effectiveValue, validating: false, errors: {} });
      return;
    }
    const controller = new AbortController();
    setAsyncValidation({ source: effectiveValue, validating: true, errors: {} });
    const timeout = window.setTimeout(() => {
      validate({ schema, value: effectiveValue, context, signal: controller.signal })
        .then((errors) => { if (!controller.signal.aborted) setAsyncValidation({ source: effectiveValue, validating: false, errors }); })
        .catch(() => { if (!controller.signal.aborted) setAsyncValidation({ source: effectiveValue, validating: false, errors: { $form: "异步校验暂时不可用" } }); });
    }, Math.max(0, validationDelayMs));
    return () => { controller.abort(); window.clearTimeout(timeout); };
  }, [context, effectiveValue, schema, validate, validation.valid, validationDelayMs]);

  const errors = useMemo(() => {
    const result: Record<string, string> = {};
    for (const issue of validation.issues) result[issue.path] ??= issueText(issue);
    return { ...result, ...currentAsyncValidation.errors, ...externalErrors };
  }, [currentAsyncValidation.errors, externalErrors, validation.issues]);
  const validationState = useMemo(() => ({
    valid: validation.valid && !currentAsyncValidation.validating && Object.keys(currentAsyncValidation.errors).length === 0 && Object.keys(externalErrors ?? {}).length === 0,
    issues: validation.issues,
    errors,
    validating: currentAsyncValidation.validating,
  }), [currentAsyncValidation, errors, externalErrors, validation]);
  useEffect(() => onValidationChange?.(validationState), [onValidationChange, validationState]);

  const update = (path: Path, next: unknown) => onChange(replaceAtPath(effectiveValue, path, next));
  const renderFields = (fields: FormField[], parent: Path = []): ReactNode => fields.filter((field) => isFormFieldVisible(field, effectiveValue)).map((field) => {
    const path = [...parent, field.key];
    const name = pathString(path);
    const current = getFormValue(effectiveValue, name);
    const disabled = Boolean(readOnly || field.readOnly || field.disabled || submitting);
    const item = (control: ReactNode) => <Form.Item
      key={name}
      label={field.title}
      extra={field.help}
      validateStatus={errors[name] === undefined ? undefined : "error"}
      help={errors[name]}
    >{control}</Form.Item>;

    switch (field.type) {
      case "textarea":
        return item(<Input.TextArea value={typeof current === "string" ? current : ""} disabled={disabled} onChange={(next) => update(path, next)} />);
      case "number":
        return item(<InputNumber value={typeof current === "number" ? current : undefined} disabled={disabled} onChange={(next) => update(path, next)} />);
      case "boolean":
        return item(<Switch checked={current === true} disabled={disabled} onChange={(next) => update(path, next)} />);
      case "select":
        return item(<Select value={optionToken(field, current)} disabled={disabled} onChange={(next) => update(path, optionValue(field, next))} options={selectOptions(field)} />);
      case "multiSelect":
        return item(<Select
          mode="multiple"
          value={Array.isArray(current) ? current.flatMap((entry) => optionToken(field, entry) ?? []) : []}
          disabled={disabled}
          onChange={(next: string[]) => update(path, next.map((token) => optionValue(field, token)))}
          options={selectOptions(field)}
        />);
      case "date":
        return item(<DatePicker value={typeof current === "string" ? current : undefined} disabled={disabled} onChange={(next) => update(path, next)} />);
      case "secretRef":
        return item(field.options?.length
          ? <Select value={optionToken(field, current)} disabled={disabled} placeholder="选择凭证引用" onChange={(next) => update(path, optionValue(field, next))} options={selectOptions(field)} />
          : <Input value={typeof current === "string" ? current : ""} autoComplete="off" placeholder="输入凭证引用 ID（不要填写明文）" disabled={disabled} onChange={(next) => update(path, next)} />);
      case "object":
        return <Card key={name} title={field.title} size="small" style={{ marginBottom: 16 }}>
          {field.help === undefined ? null : <Typography.Paragraph>{field.help}</Typography.Paragraph>}
          {renderFields(field.fields ?? [], path)}
        </Card>;
      case "array": {
        const entries = Array.isArray(current) ? current : [];
        return <Card
          key={name}
          title={field.title}
          size="small"
          style={{ marginBottom: 16 }}
          extra={<Button size="small" disabled={disabled} onClick={() => update(path, [...entries, defaultArrayEntry(field)])}>添加</Button>}
        >
          {field.help === undefined ? null : <Typography.Paragraph>{field.help}</Typography.Paragraph>}
          {errors[name] === undefined ? null : <Alert type="error" content={errors[name]} style={{ marginBottom: 12 }} />}
          {entries.length === 0 ? <Result status="404" title="暂无条目" /> : entries.map((entry, index) => <Card
            key={`${name}.${index}`}
            size="small"
            style={{ marginBottom: 12 }}
            title={`第 ${index + 1} 项`}
            extra={<Button size="mini" status="danger" disabled={disabled} onClick={() => update(path, entries.filter((_, candidate) => candidate !== index))}>删除</Button>}
          >{field.fields === undefined
              ? <Input value={typeof entry === "string" ? entry : ""} disabled={disabled} onChange={(next) => update([...path, index], next)} />
              : renderFields(field.fields, [...path, index])}
          </Card>)}
        </Card>;
      }
      case "text":
      default:
        return item(<Input value={typeof current === "string" ? current : ""} disabled={disabled} onChange={(next) => update(path, next)} />);
    }
  });

  return <Form layout="vertical">
    {currentAsyncValidation.validating ? <Alert type="info" title="正在校验" style={{ marginBottom: 16 }} /> : null}
    {Object.keys(errors).length === 0 ? null : <Alert type="error" title="表单校验未通过" content={`请检查 ${Object.keys(errors).length} 个字段`} style={{ marginBottom: 16 }} />}
    {schema.title === undefined ? null : <Typography.Title heading={6}>{schema.title}</Typography.Title>}
    {renderFields(schema.fields)}
  </Form>;
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
  FormRenderer,
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
  EmptyState: ({ title, description }) => <Result status="404" title={title} subTitle={description} />,
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
