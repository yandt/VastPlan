import type { OverlayWidth, SizeableProps } from "./component-size.js";

/** Serializable UI semantics shared by Web and Mobile renderers. */
export { uiContractMajor, uiContractRange, uiContractVersion } from "./version.generated.js";
export const interactionContractVersion = "1.0.0" as const;
export const jsonSchemaDialect = "http://json-schema.org/draft-07/schema#" as const;
export * from "./i18n.js";
export * from "./dashboard.js";
export * from "./component-size.js";

export type UICapability = "layout" | "menu" | "overlay" | "form" | "data" | "feedback" | "theme" | "approval" | "navigation";
/** Governed page-body width selected by a page and enforced by the active Shell. */
export const pageBodyLayouts = Object.freeze(["fluid", "large", "medium", "small"] as const);
export type PageBodyLayout = (typeof pageBodyLayouts)[number];

export type JSONPrimitive = string | number | boolean | null;
export type JSONValue = JSONPrimitive | readonly JSONValue[] | { readonly [key: string]: JSONValue };
/** Package-neutral JSON Schema document. V1 accepts Draft 7 only. */
export type JSONSchema = Readonly<Record<string, JSONValue>>;
/** Serializable RJSF-compatible presentation hints; never contains components or functions. */
export type FormUISchema = Readonly<Record<string, JSONValue>>;
export interface FormSchema {
  id: string;
  schema: JSONSchema;
  uiSchema?: FormUISchema;
  /** JSON Pointer -> localized text. Keeps the validation schema standards-compliant. */
  localization?: Readonly<Record<string, import("./i18n.js").LocalizedText>>;
  /** JSON Pointer -> localized presentation hint, applied only to uiSchema. */
  uiLocalization?: Readonly<Record<string, import("./i18n.js").LocalizedText>>;
}

export type FormLayout = "compact" | "horizontal" | "vertical";
export type FormLabelPlacement = "inline" | "stacked" | "inside-inline";
export const formControlAlignments = Object.freeze(["start", "end"] as const);
export type FormControlAlignment = (typeof formControlAlignments)[number];
export type FormPresentationPreset = "compact" | "standard" | "comfortable" | "guided";
export type FormWidget = "text" | "textarea" | "number" | "select" | "boolean" | "date" | "datetime" | "credentialRef" | "secretMaterial" | "hidden";
export type FormCondition =
  | { pointer: string; equals: JSONPrimitive }
  | { pointer: string; in: readonly JSONPrimitive[] }
  | { pointer: string; exists: boolean }
  | { all: readonly FormCondition[] }
  | { any: readonly FormCondition[] }
  | { not: FormCondition };
export interface FormFieldPresentation {
  pointer: string;
  span?: number;
  widget?: FormWidget;
  help?: import("./i18n.js").LocalizedText;
  visibleWhen?: FormCondition;
  readOnlyWhen?: FormCondition;
}
export interface FormSectionPresentation {
  id: string;
  title?: import("./i18n.js").LocalizedText;
  description?: import("./i18n.js").LocalizedText;
  columns?: number;
  /** Relative percentage weights. Length must equal columns and total 100. */
  columnWidths?: readonly number[];
  fields: readonly string[];
  collapsible?: boolean;
}
export interface FormPresentation extends SizeableProps {
  /** Governed visual defaults; explicit layout/label/navigation fields win. */
  preset?: FormPresentationPreset;
  layout?: FormLayout;
  /** Defaults to inline. inside-inline is reserved for compact filter-like fields. */
  labelPlacement?: FormLabelPlacement;
  /** Aligns controls within their governed field area. Defaults to end. */
  controlAlignment?: FormControlAlignment;
  /** Root object grid used when sections are not declared. */
  columns?: number;
  /** Relative percentage weights. Length must equal columns and total 100. */
  columnWidths?: readonly number[];
  navigation?: "sections" | "tabs" | "steps";
  sections?: readonly FormSectionPresentation[];
  fields?: readonly FormFieldPresentation[];
}
export interface FormWorkflow extends SizeableProps {
  /** Defaults to dialog. Page forms must opt in explicitly. */
  surface?: "page" | "dialog";
  title: import("./i18n.js").LocalizedText;
  description?: import("./i18n.js").LocalizedText;
  /** Dialog/Drawer 几何宽度，与组件 size 正交。 */
  dialogWidth?: OverlayWidth;
  /** Optional Dialog-only height in pixels; adaptive sizing remains the default. */
  dialogHeight?: number;
  submitLabel?: import("./i18n.js").LocalizedText;
  cancelLabel?: import("./i18n.js").LocalizedText;
  confirmBeforeSubmit?: import("./i18n.js").LocalizedText;
  success?: { notify?: import("./i18n.js").LocalizedText; refreshCollection?: boolean; close?: boolean };
}

export interface FormValidationIssue {
  path: string;
  code: string;
  message?: string;
  schemaPath?: string;
}
export interface FormValidationResult { valid: boolean; issues: FormValidationIssue[]; }

export { semanticIconNames } from "./icons.js";
export type { SemanticIconName } from "./icons.js";

/**
 * Serializable collection presentation. Runtime loaders and action handlers live
 * in @vastplan/workbench-sdk so this contract remains portable to Mobile/Runner.
 */
export type CollectionView = "table" | "cards";
export type CollectionQueryMode = "page" | "cursor";
export type FilterFieldKind = "text" | "select" | "boolean" | "numberRange" | "dateRange";
export type CollectionSelectionMode = "none" | "single" | "multiple";
export type CollectionDensity = "compact" | "standard" | "comfortable";
export type CollectionActionPlacement = "collection.toolbar" | "collection.bulk" | "record.row" | "record.detail" | "card.footer";
export type PageActionDisplay = "icon" | "icon-label" | "label";
export type PageActionOverflow = "auto" | "always" | "never";
export type DataValueFormat = "text" | "number" | "date" | "datetime" | "boolean" | "status";
/** 可跨 Portal、Runner 和 Mobile 使用的受治理响应式列数。 */
export type ResponsiveColumnCount = number | { xs?: number; sm?: number; md?: number; lg?: number; xl?: number };

export interface FilterOption { value: string; label: import("./i18n.js").LocalizedText; }
export interface FilterSpec {
  id: string;
  label: import("./i18n.js").LocalizedText;
  kind: FilterFieldKind;
  options?: readonly FilterOption[];
  sensitive?: boolean;
}
export interface ColumnSpec {
  key: string;
  label: import("./i18n.js").LocalizedText;
  format?: DataValueFormat;
  valueLabels?: Readonly<Record<string, import("./i18n.js").LocalizedText>>;
  statusTones?: Readonly<Record<string, "neutral" | "info" | "success" | "warning" | "error">>;
  sortable?: boolean;
  defaultVisible?: boolean;
  minWidth?: number;
  maxWidth?: number;
}
export type CollectionCardValueFormat = "text" | "number" | "date" | "datetime";
export interface CollectionCardFieldSpec {
  key: string;
  label?: import("./i18n.js").LocalizedText;
  format?: CollectionCardValueFormat;
}
export interface CollectionCardSpec extends SizeableProps {
  titleKey: string;
  subtitleKey?: string;
  status?: { labelKey: string; toneKey?: string };
  summary?: readonly CollectionCardFieldSpec[];
  content?: readonly CollectionCardFieldSpec[];
  columns?: ResponsiveColumnCount;
  loadMore?: "manual" | "viewport";
}
export interface FilterPanelLayout {
  /** 筛选区每个断点的列数；未指定时采用 xs=1、md=2、xl=4。 */
  columns?: ResponsiveColumnCount;
}
export type FilterPanelApplyMode = "auto-single-row" | "explicit";
/** 可被 Collection、MasterDetail 等工作台组合复用的一级筛选面板。 */
export interface FilterPanelSpec extends SizeableProps {
  fields: readonly FilterSpec[];
  layout?: FilterPanelLayout;
  apply?: {
    /** 默认单行直接提交、多行使用草稿；explicit 始终显示查询和清除操作。 */
    mode?: FilterPanelApplyMode;
    /** 当前仅允许受治理的末行末列位置，禁止业务插件自行注入操作区。 */
    actionsPlacement?: "last-cell";
  };
}
export interface ActionSpec {
  id: string;
  label: import("./i18n.js").LocalizedText;
  /** Required semantic icon rendered consistently by every UI framework adapter. */
  icon: import("./icons.js").SemanticIconName;
  placement: CollectionActionPlacement;
  tone?: "primary" | "secondary" | "danger";
  requiresSelection?: boolean;
  confirm?: import("./i18n.js").LocalizedText;
  form?: string;
  overlay?: string;
  /** UX projection only; Backend authorization remains authoritative. */
  requiredPermissions?: readonly string[];
  /** Evaluated only against the selected record; authorization stays server-side. */
  visibleWhen?: FormCondition;
}
/** Page-scoped command rendered independently from Collection/Record state. */
export interface PageActionSpec {
  id: string;
  label: import("./i18n.js").LocalizedText;
  icon: import("./icons.js").SemanticIconName;
  tone?: "primary" | "secondary" | "danger";
  /** Defaults to a pure icon button with Tooltip. */
  display?: PageActionDisplay;
  /** Bounded placement control; automatic overflow remains the default. */
  overflow?: PageActionOverflow;
  order?: number;
  confirm?: import("./i18n.js").LocalizedText;
  form?: string;
  overlay?: string;
  /** UX projection only; Backend authorization remains authoritative. */
  requiredPermissions?: readonly string[];
}
export interface CollectionSpec extends SizeableProps {
  id: string;
  title: import("./i18n.js").LocalizedText;
  view: CollectionView;
  query: { mode: CollectionQueryMode; defaultPageSize: number; pageSizeOptions: readonly number[] };
  filterPanel?: FilterPanelSpec;
  columns: readonly ColumnSpec[];
  card?: CollectionCardSpec;
  selection?: CollectionSelectionMode;
  actions?: readonly ActionSpec[];
  /** Table-only rendering policy. Workbench resolves framework-specific sizing and overscan. */
  table?: {
    /** auto enables row virtualization only after the governed large-data threshold. */
    virtualization?: "auto" | "always" | "off";
  };
  /** A governed presentation preference, never arbitrary CSS or framework props. */
  presentation?: { density?: CollectionDensity };
  preferences?: { allowedColumns?: readonly string[]; density?: boolean };
}

/** Framework-neutral record projection shared by detail, list-detail and tree-detail pages. */
export interface RecordFieldSpec {
  key: string;
  label: import("./i18n.js").LocalizedText;
  format?: DataValueFormat;
  valueLabels?: Readonly<Record<string, import("./i18n.js").LocalizedText>>;
  statusTones?: Readonly<Record<string, "neutral" | "info" | "success" | "warning" | "error">>;
}
export interface RecordSectionSpec {
  id: string;
  title?: import("./i18n.js").LocalizedText;
  description?: import("./i18n.js").LocalizedText;
  columns?: number;
  fields: readonly RecordFieldSpec[];
}
export interface RecordDetailSpec extends SizeableProps {
  titleKey: string;
  subtitleKey?: string;
  status?: { labelKey: string; toneKey?: string };
  sections: readonly RecordSectionSpec[];
  emptyTitle?: import("./i18n.js").LocalizedText;
}
export interface RecordMasterSpec extends SizeableProps {
  id: string;
  title: import("./i18n.js").LocalizedText;
  keyField: string;
  titleField: string;
  subtitleField?: string;
  status?: { labelField: string; toneField?: string };
  query: { mode: CollectionQueryMode; defaultPageSize: number; pageSizeOptions: readonly number[] };
  filterPanel?: FilterPanelSpec;
  selectionParam?: string;
  emptyTitle?: import("./i18n.js").LocalizedText;
}
export interface RecordTreeSpec extends SizeableProps {
  id: string;
  title: import("./i18n.js").LocalizedText;
  selectionParam?: string;
  defaultExpandedDepth?: number;
  emptyTitle?: import("./i18n.js").LocalizedText;
}

export type InteractionKind = "confirm" | "form" | "approval" | "notification" | "progress";
export type InteractionSurface = "frontend" | "mobile" | "runner.local";
export interface InteractionSource { workflowRunId?: string; capability: string; operation?: string; }
export interface InteractionRequest {
  id: string;
  contractVersion: typeof interactionContractVersion;
  kind: InteractionKind;
  source: InteractionSource;
  tenantId: string;
  eligibleSubjects: string[];
  allowedSurfaces: InteractionSurface[];
  fallback?: "expire" | "runner.local-if-allowed";
  expiresAt: string;
  title?: string;
  message?: string;
  form?: FormSchema;
}
export interface InteractionResponse {
  interactionId: string;
  decision: "answered" | "rejected";
  values?: Record<string, unknown>;
  credentialRefs?: Record<string, string>;
}

export type InteractionState = "created" | "presented" | "answered" | "rejected" | "cancelled" | "expired";
export interface InteractionAuditEvent { action: string; actorId: string; surface?: string; at: string; }

/** Persisted Broker view; it stays serializable and contains no renderer code. */
export interface InteractionRecord {
  request: InteractionRequest;
  state: InteractionState;
  response?: InteractionResponse;
  createdAt: string;
  updatedAt: string;
  presentedBy?: string;
  audit: InteractionAuditEvent[];
}
