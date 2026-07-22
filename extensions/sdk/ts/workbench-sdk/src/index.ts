import type { ActionSpec, CollectionDensity, CollectionSpec, ColumnSpec, FormPresentation, FormSchema, FormWorkflow, JSONValue, LocalizedText, RecordDetailSpec, RecordMasterSpec, RecordTreeSpec } from "@vastplan/ui-contract";

export type { ActionSpec, CollectionSpec, CollectionCardSpec, CollectionCardFieldSpec, CollectionCardValueFormat, ColumnSpec, DataValueFormat, FilterSpec, CollectionFilterKind, CollectionQueryMode, CollectionSelectionMode, CollectionView, FormCondition, FormFieldPresentation, FormLayout, FormPresentation, FormSchema, FormSectionPresentation, FormWidget, FormWorkflow, JSONValue, RecordDetailSpec, RecordFieldSpec, RecordMasterSpec, RecordSectionSpec, RecordTreeSpec } from "@vastplan/ui-contract";
export { jsonSchemaDialect, message } from "@vastplan/ui-contract";
export type { LocalizedText, MessageDescriptor, MessageValues } from "@vastplan/ui-contract";

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

export interface CollectionActionResult {
  notify?: { title: LocalizedText; content?: LocalizedText; kind?: "success" | "info" | "warning" | "error" };
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

export interface WorkbenchFormPreparation {
  schema?: FormSchema;
  presentation?: FormPresentation;
  context?: Readonly<Record<string, unknown>>;
  initialValue?: Readonly<Record<string, unknown>>;
}

export interface WorkbenchFormDefinition<Row extends Record<string, unknown> = Record<string, unknown>> {
  id: string;
  schema: FormSchema;
  presentation?: FormPresentation;
  workflow: FormWorkflow;
  initialValue?: Readonly<Record<string, unknown>>;
  context?: Readonly<Record<string, unknown>>;
  /** Resolves current enumerations/policy only when the form opens. */
  prepare?(selected: readonly Row[], signal: AbortSignal): Promise<WorkbenchFormPreparation>;
  load?(selected: readonly Row[], signal: AbortSignal): Promise<Readonly<Record<string, unknown>>>;
  validate?(request: { value: Readonly<Record<string, unknown>>; context: Readonly<Record<string, unknown>>; signal: AbortSignal }): Promise<WorkbenchFormFieldErrors>;
  submit(context: WorkbenchFormSubmitContext<Row>, signal: AbortSignal): Promise<WorkbenchFormSubmitResult | void>;
}

export type WorkbenchOverlayContent =
  | { kind: "json"; documents: readonly { title?: LocalizedText; value: JSONValue }[] }
  | { kind: "table"; columns: readonly ColumnSpec[]; rows: readonly Readonly<Record<string, unknown>>[]; rowKey?: string };

export interface WorkbenchOverlayDefinition<Row extends Record<string, unknown> = Record<string, unknown>> {
  id: string;
  surface: "dialog" | "drawer";
  title: LocalizedText;
  size?: "sm" | "md" | "lg";
  load(selected: readonly Row[], signal: AbortSignal): Promise<WorkbenchOverlayContent>;
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
  /** Hides the page when the trusted session projection lacks a permission. */
  requiredPermissions?: readonly string[];
  /** At least one permission is sufficient to expose a shared governance page. */
  requiredAnyPermissions?: readonly string[];
  navigation?: { id: string; label: LocalizedText; zone: "primary" | "settings" | "secondary"; groupID?: string; order?: number };
  collection: CollectionSpec;
  load(query: CollectionQuery, signal: AbortSignal): Promise<CollectionResult<Row>>;
  loadSummary?(signal: AbortSignal): Promise<CollectionSummary>;
  forms?: readonly WorkbenchFormDefinition<Row>[];
  overlays?: readonly WorkbenchOverlayDefinition<Row>[];
  runAction?(context: CollectionActionContext<Row>, signal: AbortSignal): Promise<CollectionActionResult | void>;
}

export interface FormPageDefinition {
  id: string;
  path: string;
  title: LocalizedText;
  description?: LocalizedText;
  requiredPermissions?: readonly string[];
  requiredAnyPermissions?: readonly string[];
  navigation?: { id: string; label: LocalizedText; zone: "primary" | "settings" | "secondary"; groupID?: string; order?: number };
  form: WorkbenchFormDefinition;
}

export interface RecordTreeNode {
  id: string;
  title: string;
  description?: string;
  status?: { label: string; tone?: "neutral" | "info" | "success" | "warning" | "error" };
  disabled?: boolean;
  children?: readonly RecordTreeNode[];
}

export interface RecordActionContext<Row extends Record<string, unknown> = Record<string, unknown>> {
  action: ActionSpec;
  record?: Readonly<Row>;
  refresh(): void;
}

interface RecordPageCommon<Row extends Record<string, unknown>> {
  id: string;
  path: string;
  title: LocalizedText;
  description?: LocalizedText;
  requiredPermissions?: readonly string[];
  requiredAnyPermissions?: readonly string[];
  navigation?: { id: string; label: LocalizedText; zone: "primary" | "settings" | "secondary"; groupID?: string; order?: number };
  detail: RecordDetailSpec;
  /** Optional page-surface editor rendered in the detail pane. */
  editor?: WorkbenchFormDefinition<Row>;
  forms?: readonly WorkbenchFormDefinition<Row>[];
  overlays?: readonly WorkbenchOverlayDefinition<Row>[];
  actions?: readonly ActionSpec[];
  runAction?(context: RecordActionContext<Row>, signal: AbortSignal): Promise<CollectionActionResult | void>;
}

export interface RecordDetailPageDefinition<Row extends Record<string, unknown> = Record<string, unknown>> extends RecordPageCommon<Row> {
  pattern: "record-detail";
  load(signal: AbortSignal): Promise<Readonly<Row> | undefined>;
}

export interface MasterDetailPageDefinition<Row extends Record<string, unknown> = Record<string, unknown>> extends RecordPageCommon<Row> {
  pattern: "master-detail";
  master: RecordMasterSpec;
  loadMaster(query: CollectionQuery, signal: AbortSignal): Promise<CollectionResult<Row>>;
  loadRecord(key: string, signal: AbortSignal): Promise<Readonly<Row> | undefined>;
}

export interface TreeDetailPageDefinition<Row extends Record<string, unknown> = Record<string, unknown>> extends RecordPageCommon<Row> {
  pattern: "tree-detail";
  tree: RecordTreeSpec;
  loadTree(signal: AbortSignal): Promise<readonly RecordTreeNode[]>;
  loadRecord(key: string, signal: AbortSignal): Promise<Readonly<Row> | undefined>;
}

export type RecordPageDefinition<Row extends Record<string, unknown> = Record<string, unknown>> =
  | RecordDetailPageDefinition<Row>
  | MasterDetailPageDefinition<Row>
  | TreeDetailPageDefinition<Row>;

/** The only registration surface a functional Collection page receives. */
export interface WorkbenchPluginContext {
  addCollectionPage<Row extends Record<string, unknown>>(page: CollectionPageDefinition<Row>): void;
  addFormPage(page: FormPageDefinition): void;
  addRecordPage<Row extends Record<string, unknown>>(page: RecordPageDefinition<Row>): void;
}

export interface WorkbenchManagementCapability { capability: string; read?: readonly string[]; write?: readonly string[]; }
export interface WorkbenchManagementService { id: string; label?: string; logicalService: string; routingDomain: string; capabilities: readonly WorkbenchManagementCapability[]; }
export interface WorkbenchPortalRuntime { revision: number; id: string; tenantId: string; route: string; experience?: { permissions: readonly string[] }; management: { services: readonly WorkbenchManagementService[] }; }
export interface WorkbenchFrontendPluginContext extends WorkbenchPluginContext {
  readonly portal: Readonly<WorkbenchPortalRuntime>;
  readonly lifecycle: Readonly<{ pluginID: string; generation: string; signal: AbortSignal; reason: "bootstrap" | "replace" | "shutdown" }>;
  readonly i18n: Readonly<{ message(key: string, fallback: string, values?: import("@vastplan/ui-contract").MessageValues): import("@vastplan/ui-contract").MessageDescriptor }>;
}

export function managementServicesFor(portal: Readonly<WorkbenchPortalRuntime>, capability: string): readonly WorkbenchManagementService[] {
  return portal.management.services.filter((service) => service.capabilities.some((grant) => grant.capability === capability));
}

/** Makes page definitions discoverable and prevents a future arbitrary component escape hatch. */
export function defineCollectionPage<Row extends Record<string, unknown>>(definition: CollectionPageDefinition<Row>): CollectionPageDefinition<Row> {
	validatePermissionRequirements(definition.requiredPermissions, "Collection page requiredPermissions");
	validatePermissionRequirements(definition.requiredAnyPermissions, "Collection page requiredAnyPermissions");
  if (definition.collection.view === "cards" && definition.collection.query.mode !== "cursor") {
    throw new Error("Card Collection 必须使用 cursor 查询");
  }
  if (definition.collection.view === "cards" && definition.collection.card === undefined) {
    throw new Error("Card Collection 必须声明 card 呈现契约");
  }
  const forms = new Map((definition.forms ?? []).map((form) => [form.id, form]));
  if (forms.size !== (definition.forms ?? []).length) throw new Error("Collection 表单 ID 必须唯一");
  const overlays = new Map((definition.overlays ?? []).map((overlay) => [overlay.id, overlay]));
  if (overlays.size !== (definition.overlays ?? []).length) throw new Error("Collection Overlay ID 必须唯一");
  for (const form of forms.values()) validateFormDefinition(form);
  for (const action of definition.collection.actions ?? []) {
		validatePermissionRequirements(action.requiredPermissions, `Action ${action.id} requiredPermissions`);
    if ((action.placement === "page.primary" || action.placement === "page.secondary") && action.icon === undefined) {
      throw new Error(`Page Action ${action.id} 必须声明语义图标`);
    }
    if (action.form !== undefined && !forms.has(action.form)) throw new Error(`Action ${action.id} 引用了未声明的表单 ${action.form}`);
    if (action.overlay !== undefined && !overlays.has(action.overlay)) throw new Error(`Action ${action.id} 引用了未声明的 Overlay ${action.overlay}`);
    if (action.form !== undefined && action.overlay !== undefined) throw new Error(`Action ${action.id} 不能同时打开表单和 Overlay`);
  }
  return Object.freeze({ ...definition, collection: Object.freeze({ ...definition.collection }) });
}

export function defineFormPage(definition: FormPageDefinition): FormPageDefinition {
	validatePermissionRequirements(definition.requiredPermissions, "Form page requiredPermissions");
	validatePermissionRequirements(definition.requiredAnyPermissions, "Form page requiredAnyPermissions");
  if (definition.form.workflow.surface !== "page") throw new Error("Form Page 的 workflow.surface 必须为 page");
  validateFormDefinition(definition.form);
  return Object.freeze({ ...definition, form: Object.freeze({ ...definition.form }) });
}

export function defineRecordDetailPage<Row extends Record<string, unknown>>(definition: RecordDetailPageDefinition<Row>): RecordDetailPageDefinition<Row> {
  validateRecordPage(definition);
  return Object.freeze({ ...definition });
}

export function defineMasterDetailPage<Row extends Record<string, unknown>>(definition: MasterDetailPageDefinition<Row>): MasterDetailPageDefinition<Row> {
  validateRecordPage(definition);
  validateMaster(definition.master);
  return Object.freeze({ ...definition });
}

export function defineTreeDetailPage<Row extends Record<string, unknown>>(definition: TreeDetailPageDefinition<Row>): TreeDetailPageDefinition<Row> {
  validateRecordPage(definition);
  if (!validIdentifier(definition.tree.id) || (definition.tree.selectionParam !== undefined && !validSelectionParam(definition.tree.selectionParam)) ||
      definition.tree.defaultExpandedDepth !== undefined && (!Number.isSafeInteger(definition.tree.defaultExpandedDepth) || definition.tree.defaultExpandedDepth < 0 || definition.tree.defaultExpandedDepth > 8)) {
    throw new Error(`TreeDetail ${definition.id} 的树定义无效`);
  }
  return Object.freeze({ ...definition });
}

function validatePermissionRequirements(values: readonly string[] | undefined, label: string): void {
	if (values === undefined) return;
	if (values.length === 0 || new Set(values).size !== values.length || values.some((value) => !/^[a-z][a-z0-9.-]{1,159}$/.test(value))) {
		throw new Error(`${label} 无效`);
	}
}

function validateRecordPage(definition: RecordPageDefinition): void {
  validatePermissionRequirements(definition.requiredPermissions, `Record page ${definition.id} requiredPermissions`);
  validatePermissionRequirements(definition.requiredAnyPermissions, `Record page ${definition.id} requiredAnyPermissions`);
  if (!validIdentifier(definition.id) || !definition.path.startsWith("/") || !validFieldKey(definition.detail.titleKey) || definition.detail.sections.length === 0) {
    throw new Error(`Record page ${definition.id} 定义无效`);
  }
  const sectionIDs = new Set<string>();
  const fieldKeys = new Set<string>();
  for (const section of definition.detail.sections) {
    if (!validIdentifier(section.id) || sectionIDs.has(section.id) || section.fields.length === 0 ||
        !Number.isSafeInteger(section.columns ?? 1) || (section.columns ?? 1) < 1 || (section.columns ?? 1) > 4) {
      throw new Error(`Record page ${definition.id} 的 section 无效或重复: ${section.id}`);
    }
    sectionIDs.add(section.id);
    for (const field of section.fields) {
      if (!validFieldKey(field.key) || fieldKeys.has(field.key)) throw new Error(`Record page ${definition.id} 的字段无效或重复: ${field.key}`);
      fieldKeys.add(field.key);
    }
  }
  for (const key of [definition.detail.subtitleKey, definition.detail.status?.labelKey, definition.detail.status?.toneKey]) {
    if (key !== undefined && !validFieldKey(key)) throw new Error(`Record page ${definition.id} 的记录字段无效: ${key}`);
  }
  if (definition.editor !== undefined) {
    if (definition.editor.workflow.surface !== "page") throw new Error(`Record page ${definition.id} 的 editor 必须使用 page surface`);
    validateFormDefinition(definition.editor);
  }
  const forms = new Map((definition.forms ?? []).map((form) => [form.id, form]));
  if (forms.size !== (definition.forms ?? []).length) throw new Error(`Record page ${definition.id} 的表单 ID 必须唯一`);
  for (const form of forms.values()) validateFormDefinition(form);
  const overlays = new Map((definition.overlays ?? []).map((overlay) => [overlay.id, overlay]));
  if (overlays.size !== (definition.overlays ?? []).length) throw new Error(`Record page ${definition.id} 的 Overlay ID 必须唯一`);
  for (const action of definition.actions ?? []) {
    validatePermissionRequirements(action.requiredPermissions, `Action ${action.id} requiredPermissions`);
    if (!validIdentifier(action.id) || !["page.primary", "page.secondary", "record.detail"].includes(action.placement)) throw new Error(`Record page ${definition.id} 的 Action 位置无效: ${action.id}`);
    if ((action.placement === "page.primary" || action.placement === "page.secondary") && action.icon === undefined) throw new Error(`Page Action ${action.id} 必须声明语义图标`);
    if (action.form !== undefined && !forms.has(action.form)) throw new Error(`Action ${action.id} 引用了未声明的表单 ${action.form}`);
    if (action.overlay !== undefined && !overlays.has(action.overlay)) throw new Error(`Action ${action.id} 引用了未声明的 Overlay ${action.overlay}`);
    if (action.form !== undefined && action.overlay !== undefined) throw new Error(`Action ${action.id} 不能同时打开表单和 Overlay`);
  }
}

function validateMaster(master: RecordMasterSpec): void {
  if (!validIdentifier(master.id) || !validFieldKey(master.keyField) || !validFieldKey(master.titleField) ||
      (master.subtitleField !== undefined && !validFieldKey(master.subtitleField)) ||
      (master.status !== undefined && (!validFieldKey(master.status.labelField) || master.status.toneField !== undefined && !validFieldKey(master.status.toneField))) ||
      (master.selectionParam !== undefined && !validSelectionParam(master.selectionParam)) ||
      !Number.isSafeInteger(master.query.defaultPageSize) || master.query.defaultPageSize < 1 || master.query.pageSizeOptions.length === 0 ||
      master.query.pageSizeOptions.some((size) => !Number.isSafeInteger(size) || size < 1)) {
    throw new Error(`MasterDetail ${master.id} 的列表定义无效`);
  }
}

function validIdentifier(value: string): boolean { return /^[A-Za-z0-9][A-Za-z0-9._-]{0,159}$/.test(value); }
function validFieldKey(value: string): boolean { return /^[A-Za-z0-9][A-Za-z0-9._-]{0,159}$/.test(value); }
function validSelectionParam(value: string): boolean { return /^[a-z][a-z0-9_-]{0,39}$/.test(value); }

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
    if (field.widget === "secretMaterial") {
      const node = schemaNode(form.schema.schema, field.pointer);
      if (node?.type !== "string" || node.format !== "vastplan-secret-material" || node.writeOnly !== true) throw new Error(`表单 ${form.id} 的 secretMaterial 字段必须声明 type=string、format=vastplan-secret-material 且 writeOnly=true`);
      if (pointerValue(form.initialValue, field.pointer).found) throw new Error(`表单 ${form.id} 的 secretMaterial 字段禁止出现在 initialValue`);
    }
  }
}

function pointerValue(root: Readonly<Record<string, unknown>> | undefined, pointer: string): { found: boolean; value?: unknown } {
  if (root === undefined) return { found: false };
  let value: unknown = root;
  for (const raw of pointer.slice(1).split("/")) {
    const key = raw.replace(/~1/g, "/").replace(/~0/g, "~");
    if (typeof value !== "object" || value === null || Array.isArray(value) || !Object.prototype.hasOwnProperty.call(value, key)) return { found: false };
    value = (value as Record<string, unknown>)[key];
  }
  return { found: true, value };
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
