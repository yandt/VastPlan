import type { ActionSpec, CollectionDensity, CollectionSpec, FormPresentation, FormSchema, FormWorkflow, LocalizedText } from "@vastplan/ui-contract";

export type { ActionSpec, CollectionSpec, CollectionCardSpec, CollectionCardFieldSpec, CollectionCardValueFormat, ColumnSpec, FilterSpec, CollectionFilterKind, CollectionQueryMode, CollectionSelectionMode, CollectionView, FormCondition, FormFieldPresentation, FormLayout, FormPresentation, FormSchema, FormSectionPresentation, FormWidget, FormWorkflow } from "@vastplan/ui-contract";

export interface CollectionQuery {
  mode: "page" | "cursor";
  page: number;
  pageSize: number;
  cursor?: string;
  filters: Readonly<Record<string, unknown>>;
  sort?: { key: string; direction: "asc" | "desc" };
}

export interface CollectionResult<Row extends Record<string, unknown> = Record<string, unknown>> {
  items: readonly Row[];
  total?: number;
  nextCursor?: string;
}

export interface CollectionActionContext<Row extends Record<string, unknown> = Record<string, unknown>> {
  action: ActionSpec;
  selected: readonly Row[];
  refresh(): void;
}

export interface CollectionSummaryMetric {
  id: string;
  label: LocalizedText;
  value: string | number;
  tone?: "neutral" | "info" | "success" | "warning" | "error";
}

export interface CollectionSummary {
  title?: LocalizedText;
  metrics: readonly CollectionSummaryMetric[];
}

export interface WorkbenchFormSubmitContext<Row extends Record<string, unknown> = Record<string, unknown>> {
  value: Readonly<Record<string, unknown>>;
  selected: readonly Row[];
}

export interface WorkbenchFormSubmitResult {
  fieldErrors?: WorkbenchFormFieldErrors;
}

/** Field errors stay semantic until Workbench resolves them for the active locale. */
export type WorkbenchFormFieldErrors = Readonly<Record<string, LocalizedText>>;

export interface WorkbenchFormDefinition<Row extends Record<string, unknown> = Record<string, unknown>> {
  id: string;
  schema: FormSchema;
  presentation?: FormPresentation;
  workflow: FormWorkflow;
  initialValue?: Readonly<Record<string, unknown>>;
  context?: Readonly<Record<string, unknown>>;
  load?(selected: readonly Row[], signal: AbortSignal): Promise<Readonly<Record<string, unknown>>>;
  validate?(request: { value: Readonly<Record<string, unknown>>; context: Readonly<Record<string, unknown>>; signal: AbortSignal }): Promise<WorkbenchFormFieldErrors>;
  submit(context: WorkbenchFormSubmitContext<Row>, signal: AbortSignal): Promise<WorkbenchFormSubmitResult | void>;
}

/** Platform Profile policy for the collection presentation family. */
export interface WorkbenchPresentationConfig {
  collection?: { defaultDensity?: CollectionDensity; allowedDensities?: readonly CollectionDensity[] };
}

export interface CollectionPageDefinition<Row extends Record<string, unknown> = Record<string, unknown>> {
  id: string;
  path: string;
  title: LocalizedText;
  description?: LocalizedText;
  navigation?: { id: string; label: LocalizedText; zone: "primary" | "settings" | "secondary"; groupID?: string; order?: number };
  collection: CollectionSpec;
  load(query: CollectionQuery, signal: AbortSignal): Promise<CollectionResult<Row>>;
  loadSummary?(signal: AbortSignal): Promise<CollectionSummary>;
  forms?: readonly WorkbenchFormDefinition<Row>[];
  runAction?(context: CollectionActionContext<Row>, signal: AbortSignal): Promise<void>;
}

export interface FormPageDefinition {
  id: string;
  path: string;
  title: LocalizedText;
  description?: LocalizedText;
  navigation?: { id: string; label: LocalizedText; zone: "primary" | "settings" | "secondary"; groupID?: string; order?: number };
  form: WorkbenchFormDefinition;
}

/** The only registration surface a functional Collection page receives. */
export interface WorkbenchPluginContext {
  addCollectionPage<Row extends Record<string, unknown>>(page: CollectionPageDefinition<Row>): void;
  addFormPage(page: FormPageDefinition): void;
}

/** Makes page definitions discoverable and prevents a future arbitrary component escape hatch. */
export function defineCollectionPage<Row extends Record<string, unknown>>(definition: CollectionPageDefinition<Row>): CollectionPageDefinition<Row> {
  if (definition.collection.view === "cards" && definition.collection.query.mode !== "cursor") {
    throw new Error("Card Collection 必须使用 cursor 查询");
  }
  if (definition.collection.view === "cards" && definition.collection.card === undefined) {
    throw new Error("Card Collection 必须声明 card 呈现契约");
  }
  const forms = new Map((definition.forms ?? []).map((form) => [form.id, form]));
  if (forms.size !== (definition.forms ?? []).length) throw new Error("Collection 表单 ID 必须唯一");
  for (const form of forms.values()) validateFormDefinition(form);
  for (const action of definition.collection.actions ?? []) {
    if (action.form !== undefined && !forms.has(action.form)) throw new Error(`Action ${action.id} 引用了未声明的表单 ${action.form}`);
  }
  return Object.freeze({ ...definition, collection: Object.freeze({ ...definition.collection }) });
}

export function defineFormPage(definition: FormPageDefinition): FormPageDefinition {
  if (definition.form.workflow.surface !== "page") throw new Error("Form Page 的 workflow.surface 必须为 page");
  validateFormDefinition(definition.form);
  return Object.freeze({ ...definition, form: Object.freeze({ ...definition.form }) });
}

function validateFormDefinition(form: WorkbenchFormDefinition): void {
  const sections = form.presentation?.sections ?? [];
  if (new Set(sections.map((section) => section.id)).size !== sections.length) throw new Error(`表单 ${form.id} 的 section ID 必须唯一`);
  for (const section of sections) {
    if (!Number.isSafeInteger(section.columns ?? 1) || (section.columns ?? 1) < 1 || (section.columns ?? 1) > 4) throw new Error(`表单 ${form.id} 的 section.columns 必须在 1..4`);
  }
  for (const field of form.presentation?.fields ?? []) {
    if (!field.pointer.startsWith("/") || field.pointer.startsWith("/context/")) throw new Error(`表单 ${form.id} 的字段 pointer 无效: ${field.pointer}`);
    if (field.span !== undefined && (!Number.isSafeInteger(field.span) || field.span < 1 || field.span > 4)) throw new Error(`表单 ${form.id} 的字段 span 必须在 1..4`);
    if (field.widget === "credentialRef") {
      const node = schemaNode(form.schema.schema, field.pointer);
      if (node?.format !== "vastplan-credential-ref" || node.writeOnly !== true) throw new Error(`表单 ${form.id} 的 credentialRef 字段必须声明 format=vastplan-credential-ref 且 writeOnly=true`);
    }
  }
}

function schemaNode(schema: Readonly<Record<string, unknown>>, pointer: string): Readonly<Record<string, unknown>> | undefined {
  let node: unknown = schema;
  for (const raw of pointer.slice(1).split("/")) {
    const key = raw.replace(/~1/g, "/").replace(/~0/g, "~");
    if (typeof node !== "object" || node === null || Array.isArray(node)) return undefined;
    const properties = (node as Record<string, unknown>).properties;
    if (typeof properties !== "object" || properties === null || Array.isArray(properties)) return undefined;
    node = (properties as Record<string, unknown>)[key];
  }
  return typeof node === "object" && node !== null && !Array.isArray(node) ? node as Readonly<Record<string, unknown>> : undefined;
}
