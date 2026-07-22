import { useEffect, useId, useLayoutEffect, useMemo, useRef, useState } from "react";
import type { MouseEvent, ReactNode } from "react";
import createCache from "@emotion/cache";
import { CacheProvider } from "@emotion/react";
import RJSFForm from "@rjsf/core";
import { customizeValidator } from "@rjsf/validator-ajv8";
import {
  Alert,
  Box,
  Breadcrumbs,
  Button as MuiButton,
  Card,
  CardContent,
  CardHeader,
  Checkbox,
  CircularProgress,
  Dialog as MuiDialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  Divider as MuiDivider,
  Drawer as MuiDrawer,
  IconButton as MuiIconButton,
  List,
  ListItemButton,
  ListItemText,
  MenuItem as MuiMenuItem,
  Pagination as MuiPagination,
  Paper,
  Popover as MuiPopover,
  ScopedCssBaseline,
  Step,
  StepLabel,
  Stepper,
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
  Tooltip,
  ThemeProvider,
  Typography,
  Chip,
  createTheme,
} from "@mui/material";
import { enUS, zhCN } from "@mui/material/locale";
import type {
  ButtonProps,
  CommandItem,
  DataCardProps,
  UIRenderer,
  DialogProps,
  DrawerProps,
  FormRendererProps,
  FormPresentation,
  FormSectionPresentation,
  GridItemProps,
  GridProps,
  IconButtonProps,
  MenuItem,
  PopoverProps,
  PortalShellProps,
  PortalUI,
  SelectProps,
  StackProps,
  StatusTone,
  TableProps,
} from "@vastplan/ui-primitives";
import { PortalUIProvider, VastPlanIcon, localizeJSONSchema, message, usePortalI18n } from "@vastplan/ui-primitives";
import { MuiNativeIcon } from "./native-icons";

const gaps = { xs: 0.5, sm: 1, md: 2, lg: 3 } as const;
const widths = { sm: "sm", md: "md", lg: "lg" } as const;
const tones: Record<StatusTone, "default" | "info" | "success" | "warning" | "error"> = {
  neutral: "default", info: "info", success: "success", warning: "warning", error: "error",
};
const namespace = "cn.vastplan.foundation.frontend.render.adapter";
const validator = customizeValidator({ customFormats: {
  "vastplan-credential-ref": /^credential:\/\/[A-Za-z0-9][A-Za-z0-9._~:/?#\[\]@!$&'()*+,;=%-]*$/,
  "vastplan-secret-material": /^.*$/s,
} });

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

function iconButtonWith(Icon: typeof VastPlanIcon, { icon, label, onClick, disabled, loading, tone = "normal" }: IconButtonProps) {
  const color = tone === "danger" ? "error" : tone === "primary" ? "primary" : "default";
  return <Tooltip title={label}><span><MuiIconButton aria-label={label} color={color} disabled={disabled || loading} onClick={onClick} sx={{ width: 44, height: 44 }}>
    {loading ? <CircularProgress size={20} /> : <Icon name={icon} />}
  </MuiIconButton></span></Tooltip>;
}

function IconButton(props: IconButtonProps) { return iconButtonWith(VastPlanIcon, props); }

function Select({ value, options, placeholder, ariaLabel, disabled, onChange }: SelectProps) {
  return <TextField select size="small" value={value ?? ""} disabled={disabled} inputProps={{ "aria-label": ariaLabel }} sx={{ minWidth: 180 }} onChange={(event) => onChange(event.target.value || undefined)}>
    {placeholder === undefined ? null : <MuiMenuItem value="" disabled>{placeholder}</MuiMenuItem>}
    {options.map((option) => <MuiMenuItem key={option.value} value={option.value} disabled={option.disabled}>{option.label}</MuiMenuItem>)}
  </TextField>;
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

interface MuiObjectFieldTemplateProps {
  fieldPathId: { path: readonly unknown[] };
  title?: string;
  description?: ReactNode;
  properties: readonly { name: string; hidden: boolean; content: ReactNode }[];
}

function MuiObjectFieldTemplate({ presentation, activeSection, onSectionChange, ...props }: MuiObjectFieldTemplateProps & { presentation?: FormPresentation; activeSection?: string; onSectionChange?(sectionID: string): void }) {
  const i18n = usePortalI18n();
  if (props.fieldPathId.path.length !== 0 || presentation?.sections === undefined || presentation.sections.length === 0) return <Box sx={{ mb: 2 }}><Typography variant="subtitle1">{props.title}</Typography>{props.description}{props.properties.filter((property) => !property.hidden).map((property) => <Box key={property.name}>{property.content}</Box>)}</Box>;
  const sections = presentation.sections;
  const selected = sections.find((section) => section.id === activeSection) ?? sections[0]!;
  const assigned = new Set(sections.flatMap((section) => section.fields.map(formFieldName)));
  const remainder = props.properties.filter((property) => !assigned.has(property.name));
  const section = (item: FormSectionPresentation) => {
    const fields = item.fields.map(formFieldName);
    const body = <><Box sx={{ display: "grid", gridTemplateColumns: `repeat(${item.columns ?? 1}, minmax(0, 1fr))`, gap: 2 }}>{props.properties.filter((property) => fields.includes(property.name) && !property.hidden).map((property) => {
      const span = presentation.fields?.find((field) => formFieldName(field.pointer) === property.name)?.span ?? 1;
      return <Box key={property.name} sx={{ gridColumn: `span ${Math.min(Math.max(1, span), item.columns ?? 1)}` }}>{property.content}</Box>;
    })}</Box></>;
    if (presentation.navigation !== "sections") return <>{item.description === undefined ? null : <Typography color="text.secondary" sx={{ mb: 2 }}>{i18n.text(item.description)}</Typography>}{body}</>;
    const content = <>{item.description === undefined ? null : <Typography color="text.secondary" sx={{ mb: 2 }}>{i18n.text(item.description)}</Typography>}{body}</>;
    return item.collapsible ? <Box component="details" sx={{ mb: 2 }}><Box component="summary" sx={{ cursor: "pointer", fontWeight: 600, mb: 1.5 }}>{item.title === undefined ? item.id : i18n.text(item.title)}</Box>{content}</Box> : <Card variant="outlined" sx={{ mb: 2 }}><CardHeader title={item.title === undefined ? undefined : i18n.text(item.title)} /><CardContent>{content}</CardContent></Card>;
  };
  const remaining = remainder.length === 0 ? null : <Box>{remainder.map((property) => <Box key={property.name}>{property.content}</Box>)}</Box>;
  if (presentation.navigation === "tabs") return <><MuiTabs value={selected.id} onChange={(_, id: string) => onSectionChange?.(id)}>{sections.map((item) => <Tab key={item.id} value={item.id} label={item.title === undefined ? item.id : i18n.text(item.title)} />)}</MuiTabs><Box sx={{ pt: 2 }}>{section(selected)}</Box>{remaining}</>;
  if (presentation.navigation === "steps") {
    const current = Math.max(0, sections.findIndex((item) => item.id === selected.id));
    return <><Stepper activeStep={current} sx={{ mb: 3 }}>{sections.map((item, index) => <Step key={item.id} onClick={() => onSectionChange?.(item.id)}><StepLabel>{item.title === undefined ? `${index + 1}` : i18n.text(item.title)}</StepLabel></Step>)}</Stepper>{section(selected)}{remaining}</>;
  }
  return <>{sections.map((item) => <Box key={item.id}>{section(item)}</Box>)}{remaining}</>;
}

function formFieldName(pointer: string): string {
  const first = pointer.startsWith("/") ? pointer.slice(1).split("/")[0] ?? "" : pointer;
  return first.replace(/~1/g, "/").replace(/~0/g, "~");
}

const emptyFormContext: Readonly<Record<string, unknown>> = Object.freeze({});

function FormRenderer({ schema, value, onChange, presentation, presentationSection, onPresentationSectionChange, readOnly, submitting, errors: externalErrors = {}, context: suppliedContext, validate, validationDelayMs = 250, onValidationChange }: FormRendererProps) {
  const i18n = usePortalI18n();
  const formContext = suppliedContext ?? emptyFormContext;
  const localizedSchema = useMemo(() => localizeJSONSchema(schema.schema, schema.localization, i18n.text), [i18n.text, schema.localization, schema.schema]);
  const localizedUISchema = useMemo(() => schema.uiSchema === undefined ? undefined : localizeJSONSchema(schema.uiSchema, schema.uiLocalization, i18n.text), [i18n.text, schema.uiLocalization, schema.uiSchema]);
  const validation = useMemo(() => validator.validateFormData(value, schema.schema), [schema, value]);
  const syncErrors = useMemo(() => Object.fromEntries(validation.errors.map((error, index) => [muiErrorPath(error) || `form.${index}`, error.message ?? i18n.text(message(namespace, "form.invalid", "值不符合 Schema"))])), [i18n, validation.errors]);
  const [asyncValidation, setAsyncValidation] = useState<{ source?: Readonly<Record<string, unknown>>; validating: boolean; errors: Readonly<Record<string, string>> }>({ validating: false, errors: {} });
  const currentAsync = asyncValidation.source === value ? asyncValidation : { source: value, validating: validate !== undefined && validation.errors.length === 0, errors: {} };
  useEffect(() => {
    if (validate === undefined || validation.errors.length > 0) { setAsyncValidation({ source: value, validating: false, errors: {} }); return; }
    const controller = new AbortController();
    setAsyncValidation({ source: value, validating: true, errors: {} });
    const timeout = window.setTimeout(() => {
      validate({ schema, value, context: formContext, signal: controller.signal })
        .then((errors) => { if (!controller.signal.aborted) setAsyncValidation({ source: value, validating: false, errors }); })
        .catch(() => { if (!controller.signal.aborted) setAsyncValidation({ source: value, validating: false, errors: { $form: i18n.text(message(namespace, "form.asyncUnavailable", "异步校验暂时不可用")) } }); });
    }, Math.max(0, validationDelayMs));
    return () => { controller.abort(); window.clearTimeout(timeout); };
  }, [formContext, i18n, schema, validate, validation.errors.length, validationDelayMs, value]);
  const combinedExternalErrors = useMemo(() => ({ ...currentAsync.errors, ...externalErrors }), [currentAsync.errors, externalErrors]);
  useEffect(() => {
    const errors = { ...syncErrors, ...combinedExternalErrors };
    onValidationChange?.({
      valid: validation.errors.length === 0 && !currentAsync.validating && Object.keys(combinedExternalErrors).length === 0,
      issues: validation.errors.map((error) => ({ path: muiErrorPath(error), code: error.name ?? "schema_invalid", message: error.message, schemaPath: error.schemaPath })),
      errors,
      validating: currentAsync.validating,
    });
  }, [combinedExternalErrors, currentAsync.validating, onValidationChange, syncErrors, validation.errors]);
  const templates = useMemo(() => ({ ObjectFieldTemplate: (props: MuiObjectFieldTemplateProps) => <MuiObjectFieldTemplate {...props} presentation={presentation} activeSection={presentationSection} onSectionChange={onPresentationSectionChange} />, ButtonTemplates: { SubmitButton: () => null } }), [onPresentationSectionChange, presentation, presentationSection]);
  return <Box sx={presentation?.layout === "horizontal" ? { "& .form-group": { display: "grid", gridTemplateColumns: "104px minmax(0, 1fr)", alignItems: "center", columnGap: 1.5 }, "& .form-group > label": { margin: 0 }, "& .form-group > .field-description": { gridColumn: "2" } } : undefined}><RJSFForm
    schema={localizedSchema}
    uiSchema={localizedUISchema}
    formData={value}
    validator={validator}
    readonly={readOnly}
    disabled={submitting}
    liveValidate="onChange"
    showErrorList="top"
    extraErrors={muiErrorSchema(combinedExternalErrors) as never}
    extraErrorsBlockSubmit
    noHtml5Validate
    onChange={(event) => onChange((event.formData ?? {}) as Record<string, unknown>)}
    templates={templates}
  ><></></RJSFForm></Box>;
}

function muiErrorPath(error: { property?: string; name?: string; params?: { missingProperty?: unknown } }): string {
  let path = error.property?.replace(/^\./, "") ?? "";
  if (error.name === "required" && typeof error.params?.missingProperty === "string") path = path === "" ? error.params.missingProperty : `${path}.${error.params.missingProperty}`;
  return path.replace(/\['([^']+)'\]/g, "$1");
}

function muiErrorSchema(errors: Readonly<Record<string, string>>): Record<string, unknown> {
  const root: Record<string, unknown> = {};
  for (const [path, value] of Object.entries(errors)) {
    const parts = path === "$form" ? [] : path.replace(/\[(\d+)\]/g, ".$1").split(".").filter(Boolean);
    let node = root;
    for (const part of parts) {
      const current = node[part];
      if (typeof current !== "object" || current === null || Array.isArray(current)) node[part] = {};
      node = node[part] as Record<string, unknown>;
    }
    node.__errors = [...(Array.isArray(node.__errors) ? node.__errors as string[] : []), value];
  }
  return root;
}

function Table({ columns, rows, rowKey = "id", selection = "none", selectedRowKeys = [], onSelectionChange, loading, empty, density = "standard", appearance = "default" }: TableProps) {
  const keyOf = (row: Readonly<Record<string, unknown>>) => typeof rowKey === "string" ? String(row[rowKey]) : rowKey(row);
  const selected = new Set(selectedRowKeys);
  const toggle = (key: string) => {
    if (selection === "single") { onSelectionChange?.(selected.has(key) ? [] : [key]); return; }
    const next = new Set(selected); next.has(key) ? next.delete(key) : next.add(key); onSelectionChange?.([...next]);
  };
  const toggleAll = () => onSelectionChange?.(selected.size === rows.length ? [] : rows.map(keyOf));
  const content = <MuiTable size={density === "compact" ? "small" : "medium"}><TableHead sx={appearance === "collection" ? { bgcolor: "action.hover" } : undefined}><TableRow>{selection === "none" ? null : <TableCell padding="checkbox"><Checkbox checked={rows.length > 0 && selected.size === rows.length} indeterminate={selected.size > 0 && selected.size < rows.length} onChange={toggleAll} inputProps={{ "aria-label": "select rows" }} /></TableCell>}{columns.map((column) => <TableCell key={column.key} sx={{ width: column.width, fontWeight: appearance === "collection" ? 600 : undefined }}>{column.title}</TableCell>)}</TableRow></TableHead><TableBody>
    {loading ? <TableRow><TableCell colSpan={columns.length}><CircularProgress size={20} /></TableCell></TableRow> : rows.length === 0 ? <TableRow><TableCell colSpan={columns.length}>{empty}</TableCell></TableRow> : rows.map((row, index) => {
      const key = keyOf(row);
      return <TableRow key={key} selected={selected.has(key)}>{selection === "none" ? null : <TableCell padding="checkbox"><Checkbox checked={selected.has(key)} onChange={() => toggle(key)} inputProps={{ "aria-label": `select ${key}` }} /></TableCell>}{columns.map((column) => <TableCell key={column.key}>{column.render?.(row[column.key], row, index) ?? String(row[column.key] ?? "")}</TableCell>)}</TableRow>;
    })}
  </TableBody></MuiTable>;
  return appearance === "collection" ? <Box sx={{ width: "100%", overflowX: "auto" }}>{content}</Box> : <Paper variant="outlined">{content}</Paper>;
}

function DataCard({ title, subtitle, status, summary, children, actions, selectable = false, selected = false, selectionLabel, density = "standard", onSelectionChange }: DataCardProps) {
  return <Card variant="outlined" sx={{ height: "100%", borderColor: selected ? "primary.main" : undefined, boxShadow: selected ? 1 : undefined }}>
    <CardHeader
      title={title}
      subheader={subtitle}
      action={<MuiStack direction="row" alignItems="center" gap={1}>{status}{selectable ? <Checkbox inputProps={{ "aria-label": selectionLabel }} checked={selected} onChange={(_, checked) => onSelectionChange?.(checked)} /> : null}</MuiStack>}
      sx={density === "compact" ? { py: 1.5 } : undefined}
    />
    <CardContent sx={density === "compact" ? { pt: 0 } : undefined}>{summary}{children}</CardContent>
    {actions === undefined ? null : <Box sx={{ px: 2, pb: density === "comfortable" ? 2.5 : 2 }}>{actions}</Box>}
  </Card>;
}

const muiThemeTemplates = Object.freeze([
  { id: "light", label: message(namespace, "theme.light", "浅色"), scheme: "light" as const },
  { id: "dark", label: message(namespace, "theme.dark", "深色"), scheme: "dark" as const },
]);
const muiIconThemes = Object.freeze([
  { id: "canonical", label: message(namespace, "iconTheme.canonical", "VastPlan 图标"), source: "canonical" as const },
  { id: "renderer-native", label: message(namespace, "iconTheme.native", "Material 原生图标"), source: "renderer-native" as const },
]);
function muiThemeTemplate(id: string | undefined) { return muiThemeTemplates.find((template) => template.id === id) ?? muiThemeTemplates[0]; }
function muiIconTheme(id: string | undefined) { return muiIconThemes.find((theme) => theme.id === id) ?? muiIconThemes[0]; }
export function muiIconForTheme(id: string | undefined) { return muiIconTheme(id).source === "renderer-native" ? MuiNativeIcon : VastPlanIcon; }
type MuiComponents = Omit<PortalUI, "notify" | "confirm">;

export const muiPortalUIComponents: MuiComponents = {
  PortalShell, Page, Panel, Stack, Grid, GridItem,
  Divider: ({ label }) => <MuiDivider>{label}</MuiDivider>,
  Button: ({ children, kind, loading, ...props }) => <MuiButton {...buttonVariant(kind)} {...props}>{loading ? <CircularProgress size={16} /> : children}</MuiButton>,
  IconButton,
  Select,
  Menu: ({ items, activeID, onSelect }) => <List><MenuBranch items={items} activeID={activeID} onSelect={onSelect} /></List>,
  Breadcrumb: ({ items }) => <Breadcrumbs>{items.map((item) => <MuiButton key={item.id} href={item.href} onClick={item.onSelect} variant="text">{item.label}</MuiButton>)}</Breadcrumbs>,
  Tabs: ({ items, activeID, onChange }) => <><MuiTabs value={activeID ?? false} onChange={(_, id: string) => onChange?.(id)}>{items.map((item) => <Tab key={item.id} value={item.id} label={item.label} disabled={item.disabled} />)}</MuiTabs>{items.find((item) => item.id === activeID)?.content}</>,
  CommandPalette, Popover, Dialog, Drawer, FormRenderer,
  FilterBar: ({ children, actions, appearance = "default" }) => appearance === "collection"
    ? <Box sx={{ display: "flex", gap: 3, alignItems: "stretch", flexWrap: "wrap", pb: 3, borderBottom: 1, borderColor: "divider" }}><Box sx={{ flex: "1 1 720px", minWidth: 0 }}>{children}</Box>{actions === undefined ? null : <Box sx={{ display: "flex", alignItems: "stretch", pl: 3, borderLeft: 1, borderColor: "divider" }}>{actions}</Box>}</Box>
    : <Paper variant="outlined" sx={{ p: 2 }}><MuiStack direction="row" gap={2} alignItems="center" flexWrap="wrap">{children}<Box sx={{ ml: "auto" }}>{actions}</Box></MuiStack></Paper>,
  Table,
  DataCard,
  Pagination: ({ page, total, pageSize, disabled, align = "start", onChange }) => <Box sx={{ display: "flex", justifyContent: align === "end" ? "flex-end" : align === "center" ? "center" : "flex-start" }}><MuiPagination page={page} count={Math.max(1, Math.ceil(total / pageSize))} disabled={disabled} onChange={(_, next) => onChange(next, pageSize)} /></Box>,
  Descriptions: ({ title, items, columns = 2 }) => <Box><Typography variant="h6">{title}</Typography><Grid columns={columns}>{items.map((item) => <Box key={item.id}><Typography variant="caption" color="text.secondary">{item.label}</Typography><Typography>{item.value}</Typography></Box>)}</Grid></Box>,
  Status: ({ tone = "neutral", children }) => <Chip color={tones[tone]} label={children} size="small" />,
  Icon: VastPlanIcon,
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

function MuiProvider({ children, locale, direction, themeTemplate, iconTheme }: { children: ReactNode; locale: string; direction: "ltr" | "rtl"; themeTemplate?: string; iconTheme?: string }) {
  const activeTemplate = muiThemeTemplate(themeTemplate);
  const activeIconTheme = muiIconTheme(iconTheme);
  const ActiveIcon = muiIconForTheme(activeIconTheme.id);
  const i18n = usePortalI18n();
  const boundary = useRef<HTMLDivElement>(null);
  const [shadowRuntime, setShadowRuntime] = useState<{ cache: ReturnType<typeof createCache>; theme: ReturnType<typeof createTheme> }>();
  const [notice, setNotice] = useState<{ title: string; content?: string; kind: "info" | "success" | "warning" | "error" }>();
  const [confirmation, setConfirmation] = useState<{ title: string; content?: string; resolve(value: boolean): void }>();
  const ui = useMemo<PortalUI>(() => ({
    ...muiPortalUIComponents,
    Icon: ActiveIcon,
    IconButton: (props) => iconButtonWith(ActiveIcon, props),
    notify: ({ title, content, kind = "info" }) => setNotice({ title, content, kind }),
    confirm: ({ title, content }) => new Promise((resolve) => setConfirmation({ title, content, resolve })),
    theme: { ...muiPortalUIComponents.theme, mode: activeTemplate.scheme === "dark" ? "dark" : "light" },
  }), [ActiveIcon, activeTemplate.scheme]);
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
      theme: createTheme({ direction, palette: { mode: activeTemplate.scheme === "dark" ? "dark" : "light" }, components: {
        MuiModal: { defaultProps: { container: overlayContainer } },
        MuiPopover: { defaultProps: { container: overlayContainer } },
        MuiPopper: { defaultProps: { container: overlayContainer } },
      } }, locale.toLowerCase().startsWith("zh") ? zhCN : enUS),
    });
  }, [activeTemplate.id, activeTemplate.scheme, direction, locale]);
  return <div ref={boundary} data-vastplan-design-system="mui" data-vastplan-theme-template={activeTemplate.id} data-vastplan-icon-theme={activeIconTheme.id} lang={locale} dir={direction}>{shadowRuntime === undefined ? null : <CacheProvider value={shadowRuntime.cache}><ThemeProvider theme={shadowRuntime.theme}><ScopedCssBaseline><PortalUIProvider ui={ui}>{children}</PortalUIProvider>
    <Snackbar open={notice !== undefined} autoHideDuration={5000} onClose={() => setNotice(undefined)}><Alert severity={notice?.kind ?? "info"} onClose={() => setNotice(undefined)}><strong>{notice?.title}</strong>{notice?.content === undefined ? null : ` — ${notice.content}`}</Alert></Snackbar>
    <MuiDialog open={confirmation !== undefined} onClose={() => finishConfirmation(false)}><DialogTitle>{confirmation?.title}</DialogTitle><DialogContent>{confirmation?.content}</DialogContent><DialogActions><MuiButton onClick={() => finishConfirmation(false)}>{i18n.text(message(namespace, "action.cancel", "取消"))}</MuiButton><MuiButton variant="contained" onClick={() => finishConfirmation(true)}>{i18n.text(message(namespace, "action.confirm", "确认"))}</MuiButton></DialogActions></MuiDialog>
  </ScopedCssBaseline></ThemeProvider></CacheProvider>}</div>;
}

export const muiRenderer: UIRenderer = {
  id: "mui",
  label: { namespace: "cn.vastplan.foundation.frontend.render.adapter", key: "renderer.mui", fallback: "Material UI" },
  framework: "mui",
  capabilities: ["layout", "menu", "overlay", "form", "data", "feedback", "theme", "navigation"],
  themeTemplates: muiThemeTemplates,
  defaultThemeTemplate: "light",
  iconThemes: muiIconThemes,
  defaultIconTheme: "canonical",
  Provider: MuiProvider,
  localization: {
    defaultLocale: "zh-CN",
    messages: {
      "zh-CN": { "command.title": "命令", "command.search": "搜索命令", "action.retry": "重试", "action.cancel": "取消", "action.confirm": "确认", "theme.light":"浅色", "theme.dark":"深色", "iconTheme.canonical":"VastPlan 图标", "iconTheme.native":"Material 原生图标", "form.invalid": "值不符合 Schema" },
      "en-US": { "command.title": "Commands", "command.search": "Search commands", "action.retry": "Retry", "action.cancel": "Cancel", "action.confirm": "Confirm", "theme.light":"Light", "theme.dark":"Dark", "iconTheme.canonical":"VastPlan Icons", "iconTheme.native":"Native Material Icons", "form.invalid": "Value does not match the schema" },
    },
  },
};

/** Explicit module role consumed only through the unified Adapter catalog. */
export const renderer = muiRenderer;

/** @deprecated Internal renderer source. Use the unified render-adapter plugin. */
export const muiRenderAdapter = muiRenderer;
export default muiRenderer;
