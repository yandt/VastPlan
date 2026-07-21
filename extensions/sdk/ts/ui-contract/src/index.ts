/** Serializable UI semantics shared by Web and Mobile renderers. */
export const uiContractVersion = "4.0.0" as const;
export const interactionContractVersion = "1.0.0" as const;
export const jsonSchemaDialect = "http://json-schema.org/draft-07/schema#" as const;
export * from "./i18n.js";

export type UICapability = "layout" | "menu" | "overlay" | "form" | "data" | "feedback" | "theme" | "approval" | "navigation";

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
export type FormWidget = "text" | "textarea" | "number" | "select" | "boolean" | "date" | "datetime" | "credentialRef" | "hidden";
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
  fields: readonly string[];
  collapsible?: boolean;
}
export interface FormPresentation {
  layout?: FormLayout;
  navigation?: "sections" | "tabs" | "steps";
  sections?: readonly FormSectionPresentation[];
  fields?: readonly FormFieldPresentation[];
}
export interface FormWorkflow {
  surface: "page" | "dialog" | "drawer";
  title: import("./i18n.js").LocalizedText;
  description?: import("./i18n.js").LocalizedText;
  size?: "sm" | "md" | "lg";
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

/**
 * Serializable collection presentation. Runtime loaders and action handlers live
 * in @vastplan/workbench-sdk so this contract remains portable to Mobile/Runner.
 */
export type CollectionView = "table" | "cards";
export type CollectionQueryMode = "page" | "cursor";
export type CollectionFilterKind = "text" | "select" | "boolean" | "numberRange" | "dateRange";
export type CollectionSelectionMode = "none" | "single" | "multiple";
export type CollectionDensity = "compact" | "standard" | "comfortable";
export type CollectionActionPlacement = "page.primary" | "page.secondary" | "collection.toolbar" | "collection.bulk" | "record.row" | "card.footer";

export interface FilterOption { value: string; label: import("./i18n.js").LocalizedText; }
export interface FilterSpec {
  id: string;
  label: import("./i18n.js").LocalizedText;
  kind: CollectionFilterKind;
  options?: readonly FilterOption[];
  sensitive?: boolean;
}
export interface ColumnSpec {
  key: string;
  label: import("./i18n.js").LocalizedText;
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
export interface CollectionCardSpec {
  titleKey: string;
  subtitleKey?: string;
  status?: { labelKey: string; toneKey?: string };
  summary?: readonly CollectionCardFieldSpec[];
  content?: readonly CollectionCardFieldSpec[];
  columns?: { xs?: number; sm?: number; md?: number; lg?: number; xl?: number };
  loadMore?: "manual" | "viewport";
}
export interface ActionSpec {
  id: string;
  label: import("./i18n.js").LocalizedText;
  placement: CollectionActionPlacement;
  tone?: "primary" | "secondary" | "danger";
  requiresSelection?: boolean;
  confirm?: import("./i18n.js").LocalizedText;
  form?: string;
}
export interface CollectionSpec {
  id: string;
  title: import("./i18n.js").LocalizedText;
  view: CollectionView;
  query: { mode: CollectionQueryMode; defaultPageSize: number; pageSizeOptions: readonly number[] };
  filters?: readonly FilterSpec[];
  columns: readonly ColumnSpec[];
  card?: CollectionCardSpec;
  selection?: CollectionSelectionMode;
  actions?: readonly ActionSpec[];
  /** A governed presentation preference, never arbitrary CSS or framework props. */
  presentation?: { density?: CollectionDensity };
  preferences?: { allowedColumns?: readonly string[]; density?: boolean };
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
