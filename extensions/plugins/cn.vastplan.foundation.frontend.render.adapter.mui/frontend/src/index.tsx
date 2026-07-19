import { useEffect, useId, useLayoutEffect, useMemo, useRef, useState } from "react";
import type { MouseEvent, ReactNode } from "react";
import createCache from "@emotion/cache";
import { CacheProvider } from "@emotion/react";
import RJSFForm from "@rjsf/core";
import validator from "@rjsf/validator-ajv8";
import {
  Alert,
  Box,
  Breadcrumbs,
  Button as MuiButton,
  Card,
  CardContent,
  CardHeader,
  CircularProgress,
  Dialog as MuiDialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  Divider as MuiDivider,
  Drawer as MuiDrawer,
  List,
  ListItemButton,
  ListItemText,
  Pagination as MuiPagination,
  Paper,
  Popover as MuiPopover,
  ScopedCssBaseline,
  Skeleton as MuiSkeleton,
  Snackbar,
  Stack as MuiStack,
  Tab,
  Table as MuiTable,
  TableBody,
  TableCell,
  TableHead,
  TableRow,
  Tabs as MuiTabs,
  TextField,
  ThemeProvider,
  Typography,
  Chip,
  createTheme,
} from "@mui/material";
import { enUS, zhCN } from "@mui/material/locale";
import type {
  ButtonProps,
  CommandItem,
  UIRenderAdapter,
  DialogProps,
  DrawerProps,
  FormRendererProps,
  GridItemProps,
  GridProps,
  MenuItem,
  PopoverProps,
  PortalShellProps,
  PortalUI,
  SemanticIconName,
  StackProps,
  StatusTone,
  TableProps,
} from "@vastplan/ui-primitives";
import { PortalUIProvider, localizeJSONSchema, message, usePortalI18n } from "@vastplan/ui-primitives";

const gaps = { xs: 0.5, sm: 1, md: 2, lg: 3 } as const;
const widths = { sm: "sm", md: "md", lg: "lg" } as const;
const tones: Record<StatusTone, "default" | "info" | "success" | "warning" | "error"> = {
  neutral: "default", info: "info", success: "success", warning: "warning", error: "error",
};

function PortalShell({ header, navigation, inspector, statusBar, children }: PortalShellProps) {
  return <Box sx={{ minHeight: "100%", display: "grid", gridTemplateRows: "auto 1fr auto" }}>
    {header}<Box sx={{ display: "grid", gridTemplateColumns: `${navigation === undefined ? 0 : 240}px minmax(0, 1fr) ${inspector === undefined ? 0 : 320}px` }}>
      {navigation}<Box component="main">{children}</Box>{inspector}
    </Box>{statusBar}
  </Box>;
}

function Page({ title, actions, children }: { title?: string; actions?: ReactNode; children: ReactNode }) {
  return <Box component="main" sx={{ p: 3 }}>
    {title === undefined && actions === undefined ? null : <MuiStack direction="row" justifyContent="space-between" alignItems="center" spacing={2} sx={{ mb: 2 }}>
      {title === undefined ? <span /> : <Typography variant="h5">{title}</Typography>}{actions}
    </MuiStack>}{children}
  </Box>;
}

function Panel({ title, children }: { title?: string; children: ReactNode }) {
  return <Card>{title === undefined ? null : <CardHeader title={title} /> }<CardContent>{children}</CardContent></Card>;
}

function Stack({ direction = "column", gap = "md", align = "stretch", justify = "start", wrap = false, children }: StackProps) {
  const justifyContent = justify === "between" ? "space-between" : justify === "start" ? "flex-start" : `flex-${justify}`;
  const alignItems = align === "start" || align === "end" ? `flex-${align}` : align;
  return <MuiStack direction={direction} gap={gaps[gap]} alignItems={alignItems} justifyContent={justifyContent} flexWrap={wrap ? "wrap" : "nowrap"}>{children}</MuiStack>;
}

function responsiveColumns(columns: GridProps["columns"]): string | Record<string, string> {
  if (typeof columns === "number" || columns === undefined) return `repeat(${columns ?? 1}, minmax(0, 1fr))`;
  return Object.fromEntries(Object.entries(columns).map(([key, count]) => [key, `repeat(${count}, minmax(0, 1fr))`]));
}

function Grid({ columns = 1, gap = "md", children }: GridProps) {
  return <Box sx={{ display: "grid", gridTemplateColumns: responsiveColumns(columns), gap: gaps[gap] }}>{children}</Box>;
}

function GridItem({ span = 1, children }: GridItemProps) {
  const gridColumn = typeof span === "number" ? `span ${span}` : Object.fromEntries(Object.entries(span).map(([key, value]) => [key, `span ${value}`]));
  return <Box sx={{ gridColumn }}>{children}</Box>;
}

function MenuBranch({ items, activeID, onSelect, depth = 0 }: { items: MenuItem[]; activeID?: string; onSelect?(id: string): void; depth?: number }) {
  return <>{items.map((item) => <Box key={item.id}>
    <ListItemButton component={item.href === undefined ? "div" : "a"} href={item.href} selected={activeID === item.id} disabled={item.disabled} onClick={(event: MouseEvent<HTMLElement>) => {
      if (item.href !== undefined) event.preventDefault();
      if (!item.children?.length) onSelect?.(item.id);
    }} sx={{ pl: 2 + depth * 2 }}>
      {item.icon}<ListItemText primary={item.label} />
    </ListItemButton>
    {item.children?.length ? <MenuBranch items={item.children} activeID={activeID} onSelect={onSelect} depth={depth + 1} /> : null}
  </Box>)}</>;
}

function CommandPalette({ open, commands, query, onQueryChange, onClose }: { open: boolean; commands: CommandItem[]; query: string; onQueryChange(query: string): void; onClose(): void }) {
  const i18n = usePortalI18n();
  const term = query.trim().toLocaleLowerCase();
  const visible = term === "" ? commands : commands.filter((command) => [command.title, command.description, ...(command.keywords ?? [])].some((value) => value?.toLocaleLowerCase().includes(term)));
  return <MuiDialog open={open} onClose={onClose} fullWidth maxWidth="sm"><DialogTitle>{i18n.text(message(namespace, "command.title", "命令"))}</DialogTitle><DialogContent>
    <TextField autoFocus fullWidth value={query} label={i18n.text(message(namespace, "command.search", "搜索命令"))} onChange={(event) => onQueryChange(event.target.value)} sx={{ my: 1 }} />
    <List>{visible.map((command) => <ListItemButton key={command.id} disabled={command.disabled} onClick={() => { command.run(); onClose(); }}><ListItemText primary={command.title} secondary={command.description} /></ListItemButton>)}</List>
  </DialogContent></MuiDialog>;
}

function Dialog({ open, title, children, footer, width = "md", onClose }: DialogProps) {
  return <MuiDialog open={open} onClose={onClose} fullWidth maxWidth={widths[width]}><DialogTitle>{title}</DialogTitle><DialogContent>{children}</DialogContent>{footer === undefined ? null : <DialogActions>{footer}</DialogActions>}</MuiDialog>;
}

function Popover({ open, trigger, children, placement = "bottom-start", initialFocus = "first", ariaLabel, onOpenChange }: PopoverProps) {
  const contentID = useId();
  const triggerRef = useRef<HTMLElement | null>(null);
  const contentRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!open || initialFocus === "none") return;
    const selector = initialFocus === "current" ? "[aria-current='page']" : "button,a[href],[tabindex]:not([tabindex='-1'])";
    const target = contentRef.current?.querySelector<HTMLElement>(selector) ?? contentRef.current?.querySelector<HTMLElement>("button,a[href],[tabindex]:not([tabindex='-1'])");
    target?.focus();
  }, [initialFocus, open]);
  const horizontal = placement.endsWith("-start") ? "left" : placement.endsWith("-end") ? "right" : "center";
  const vertical = placement.startsWith("top") ? "top" : "bottom";
  const close = (reason: "escape" | "outside") => {
    onOpenChange(false, reason);
    triggerRef.current?.focus();
  };
  return <>{trigger({
    ref: (node) => { triggerRef.current = node; },
    "aria-expanded": open,
    "aria-controls": contentID,
    onClick: () => onOpenChange(!open, "trigger"),
    onKeyDown: (event) => {
      if (event.key === "ArrowDown") { event.preventDefault(); onOpenChange(true, "trigger"); }
      if (event.key === "Escape" && open) { event.preventDefault(); close("escape"); }
    },
  })}<MuiPopover
    open={open}
    anchorEl={triggerRef.current}
    onClose={(_, reason) => close(reason === "escapeKeyDown" ? "escape" : "outside")}
    anchorOrigin={{ vertical, horizontal }}
    transformOrigin={{ vertical: vertical === "top" ? "bottom" : "top", horizontal }}
  ><Box id={contentID} ref={contentRef} role="region" aria-label={ariaLabel}>{children}</Box></MuiPopover></>;
}

function Drawer({ open, title, children, footer, width = "md", placement = "right", onClose }: DrawerProps) {
  const size = width === "sm" ? 480 : width === "md" ? 720 : 960;
  const horizontal = placement === "left" || placement === "right";
  return <MuiDrawer open={open} anchor={placement} onClose={onClose} slotProps={{ paper: { sx: horizontal ? { width: size, maxWidth: "100vw" } : { height: size, maxHeight: "100vh" } } }}>
    <Box sx={{ p: 2 }}><Typography variant="h6">{title}</Typography></Box><MuiDivider /><Box sx={{ p: 2, flex: 1, overflow: "auto" }}>{children}</Box>{footer === undefined ? null : <Box sx={{ p: 2 }}>{footer}</Box>}
  </MuiDrawer>;
}

function buttonVariant(kind: ButtonProps["kind"]): { variant: "contained" | "outlined" | "text"; color?: "error" } {
  if (kind === "primary") return { variant: "contained" };
  if (kind === "danger") return { variant: "outlined", color: "error" };
  if (kind === "text") return { variant: "text" };
  return { variant: "outlined" };
}

function FormRenderer({ schema, value, onChange, readOnly, submitting, onValidationChange }: FormRendererProps) {
  const i18n = usePortalI18n();
  const localizedSchema = useMemo(() => localizeJSONSchema(schema.schema, schema.localization, i18n.text), [i18n.text, schema.localization, schema.schema]);
  const localizedUISchema = useMemo(() => schema.uiSchema === undefined ? undefined : localizeJSONSchema(schema.uiSchema, schema.uiLocalization, i18n.text), [i18n.text, schema.uiLocalization, schema.uiSchema]);
  const validation = useMemo(() => validator.validateFormData(value, schema.schema), [schema, value]);
  useLayoutEffect(() => {
    const errors = Object.fromEntries(validation.errors.map((error, index) => [error.property || `form.${index}`, error.message ?? i18n.text(message(namespace, "form.invalid", "值不符合 Schema"))]));
    onValidationChange?.({
      valid: validation.errors.length === 0,
      issues: validation.errors.map((error) => ({ path: error.property ?? "", code: error.name ?? "schema_invalid", message: error.message, schemaPath: error.schemaPath })),
      errors,
      validating: false,
    });
  }, [i18n, onValidationChange, validation]);
  return <RJSFForm
    schema={localizedSchema}
    uiSchema={localizedUISchema}
    formData={value}
    validator={validator}
    readonly={readOnly}
    disabled={submitting}
    onChange={(event) => onChange((event.formData ?? {}) as Record<string, unknown>)}
    templates={{ ButtonTemplates: { SubmitButton: () => null } }}
  ><></></RJSFForm>;
}

function Table({ columns, rows, rowKey = "id", loading, empty }: TableProps) {
  return <Paper variant="outlined"><MuiTable><TableHead><TableRow>{columns.map((column) => <TableCell key={column.key} sx={{ width: column.width }}>{column.title}</TableCell>)}</TableRow></TableHead><TableBody>
    {loading ? <TableRow><TableCell colSpan={columns.length}><CircularProgress size={20} /></TableCell></TableRow> : rows.length === 0 ? <TableRow><TableCell colSpan={columns.length}>{empty}</TableCell></TableRow> : rows.map((row, index) => {
      const key = typeof rowKey === "string" ? String(row[rowKey]) : rowKey(row);
      return <TableRow key={key}>{columns.map((column) => <TableCell key={column.key}>{column.render?.(row[column.key], row, index) ?? String(row[column.key] ?? "")}</TableCell>)}</TableRow>;
    })}
  </TableBody></MuiTable></Paper>;
}

const symbols: Record<SemanticIconName, string> = { add: "+", remove: "−", edit: "✎", search: "⌕", settings: "⚙", success: "✓", warning: "!", error: "×", info: "i", close: "×", menu: "☰" };
type MuiComponents = Omit<PortalUI, "notify" | "confirm">;

export const muiPortalUIComponents: MuiComponents = {
  PortalShell, Page, Panel, Stack, Grid, GridItem,
  Divider: ({ label }) => <MuiDivider>{label}</MuiDivider>,
  Button: ({ children, kind, loading, ...props }) => <MuiButton {...buttonVariant(kind)} {...props}>{loading ? <CircularProgress size={16} /> : children}</MuiButton>,
  Menu: ({ items, activeID, onSelect }) => <List><MenuBranch items={items} activeID={activeID} onSelect={onSelect} /></List>,
  Breadcrumb: ({ items }) => <Breadcrumbs>{items.map((item) => <MuiButton key={item.id} href={item.href} onClick={item.onSelect} variant="text">{item.label}</MuiButton>)}</Breadcrumbs>,
  Tabs: ({ items, activeID, onChange }) => <><MuiTabs value={activeID ?? false} onChange={(_, id: string) => onChange?.(id)}>{items.map((item) => <Tab key={item.id} value={item.id} label={item.label} disabled={item.disabled} />)}</MuiTabs>{items.find((item) => item.id === activeID)?.content}</>,
  CommandPalette, Popover, Dialog, Drawer, FormRenderer,
  FilterBar: ({ children, actions }) => <Paper variant="outlined" sx={{ p: 2 }}><MuiStack direction="row" gap={2} alignItems="center" flexWrap="wrap">{children}<Box sx={{ ml: "auto" }}>{actions}</Box></MuiStack></Paper>,
  Table,
  Pagination: ({ page, total, pageSize, disabled, onChange }) => <MuiPagination page={page} count={Math.max(1, Math.ceil(total / pageSize))} disabled={disabled} onChange={(_, next) => onChange(next, pageSize)} />,
  Descriptions: ({ title, items, columns = 2 }) => <Box><Typography variant="h6">{title}</Typography><Grid columns={columns}>{items.map((item) => <Box key={item.id}><Typography variant="caption" color="text.secondary">{item.label}</Typography><Typography>{item.value}</Typography></Box>)}</Grid></Box>,
  Status: ({ tone = "neutral", children }) => <Chip color={tones[tone]} label={children} size="small" />,
  Icon: ({ name, label, size = "md" }) => <Box component="span" aria-label={label} aria-hidden={label === undefined} sx={{ fontSize: size === "sm" ? 14 : size === "md" ? 16 : 20 }}>{symbols[name]}</Box>,
  theme: { mode: "system", tokens: {
    color: { canvas: "#fafafa", surface: "#fff", overlaySurface: "#fff", text: "#1d2129", mutedText: "#6b7785", border: "#d9d9d9", primary: "#1976d2", danger: "#d32f2f", warning: "#ed6c02", success: "#2e7d32", hover: "rgba(25,118,210,.06)", selected: "rgba(25,118,210,.12)", focusRing: "#1976d2" },
    radius: { sm: 4, md: 8, lg: 12 }, spacing: { xs: 4, sm: 8, md: 16, lg: 24 },
    shell: { barHeight: 64, railWidth: 64, navigationWidth: 240, navigationCompactWidth: 220 },
    overlay: { navigationMinWidth: 480, navigationMaxWidth: 840 }, elevation: { overlay: "0 8px 24px rgba(0,0,0,.12)" }, motion: { fast: 120, normal: 180 }, focus: { width: 2 }, touch: { minimum: 44 },
  } },
  EmptyState: ({ title, description }) => <Box sx={{ p: 4, textAlign: "center" }}><Typography variant="h6">{title}</Typography><Typography color="text.secondary">{description}</Typography></Box>,
  ErrorState: function ErrorState({ title, retry }) { const i18n = usePortalI18n(); return <Alert severity="error" action={retry === undefined ? undefined : <MuiButton onClick={retry}>{i18n.text(message(namespace, "action.retry", "重试"))}</MuiButton>}>{title}</Alert>; },
  Skeleton: ({ rows = 3 }) => <MuiStack gap={1}>{Array.from({ length: rows }, (_, index) => <MuiSkeleton key={index} />)}</MuiStack>,
  Busy: ({ label }) => <MuiStack direction="row" gap={1} alignItems="center"><CircularProgress size={20} /><span>{label}</span></MuiStack>,
};

function MuiProvider({ children, locale, direction }: { children: ReactNode; locale: string; direction: "ltr" | "rtl" }) {
  const i18n = usePortalI18n();
  const boundary = useRef<HTMLDivElement>(null);
  const [shadowRuntime, setShadowRuntime] = useState<{ cache: ReturnType<typeof createCache>; theme: ReturnType<typeof createTheme> }>();
  const [notice, setNotice] = useState<{ title: string; content?: string; kind: "info" | "success" | "warning" | "error" }>();
  const [confirmation, setConfirmation] = useState<{ title: string; content?: string; resolve(value: boolean): void }>();
  const ui = useMemo<PortalUI>(() => ({
    ...muiPortalUIComponents,
    notify: ({ title, content, kind = "info" }) => setNotice({ title, content, kind }),
    confirm: ({ title, content }) => new Promise((resolve) => setConfirmation({ title, content, resolve })),
  }), []);
  const finishConfirmation = (accepted: boolean) => {
    confirmation?.resolve(accepted);
    setConfirmation(undefined);
  };
  useLayoutEffect(() => {
    if (boundary.current === null) return;
    const root = boundary.current.getRootNode();
    const styleContainer = typeof ShadowRoot !== "undefined" && root instanceof ShadowRoot ? root : boundary.current;
    const overlayContainer = boundary.current;
    setShadowRuntime({
      cache: createCache({ key: "vastplan-mui", container: styleContainer, prepend: true }),
      theme: createTheme({ direction, components: {
        MuiModal: { defaultProps: { container: overlayContainer } },
        MuiPopover: { defaultProps: { container: overlayContainer } },
        MuiPopper: { defaultProps: { container: overlayContainer } },
      } }, locale.toLowerCase().startsWith("zh") ? zhCN : enUS),
    });
  }, [direction, locale]);
  return <div ref={boundary} data-vastplan-design-system="mui" lang={locale} dir={direction}>{shadowRuntime === undefined ? null : <CacheProvider value={shadowRuntime.cache}><ThemeProvider theme={shadowRuntime.theme}><ScopedCssBaseline><PortalUIProvider ui={ui}>{children}</PortalUIProvider>
    <Snackbar open={notice !== undefined} autoHideDuration={5000} onClose={() => setNotice(undefined)}><Alert severity={notice?.kind ?? "info"} onClose={() => setNotice(undefined)}><strong>{notice?.title}</strong>{notice?.content === undefined ? null : ` — ${notice.content}`}</Alert></Snackbar>
    <MuiDialog open={confirmation !== undefined} onClose={() => finishConfirmation(false)}><DialogTitle>{confirmation?.title}</DialogTitle><DialogContent>{confirmation?.content}</DialogContent><DialogActions><MuiButton onClick={() => finishConfirmation(false)}>{i18n.text(message(namespace, "action.cancel", "取消"))}</MuiButton><MuiButton variant="contained" onClick={() => finishConfirmation(true)}>{i18n.text(message(namespace, "action.confirm", "确认"))}</MuiButton></DialogActions></MuiDialog>
  </ScopedCssBaseline></ThemeProvider></CacheProvider>}</div>;
}

export const muiRenderAdapter: UIRenderAdapter = {
  id: "ui.render.adapter",
  framework: "mui",
  uiContract: "3.0.0",
  capabilities: ["layout", "menu", "overlay", "form", "data", "feedback", "theme", "navigation"],
  Provider: MuiProvider,
  localization: {
    defaultLocale: "zh-CN",
    messages: {
      "zh-CN": { "command.title": "命令", "command.search": "搜索命令", "action.retry": "重试", "action.cancel": "取消", "action.confirm": "确认", "form.invalid": "值不符合 Schema" },
      "en-US": { "command.title": "Commands", "command.search": "Search commands", "action.retry": "Retry", "action.cancel": "Cancel", "action.confirm": "Confirm", "form.invalid": "Value does not match the schema" },
    },
  },
};

const namespace = "cn.vastplan.foundation.frontend.render.adapter.mui";

export default muiRenderAdapter;
