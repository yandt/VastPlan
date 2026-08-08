import type { ActionSpec, CollectionDensity, CollectionSpec, ColumnSpec, ComponentSize, FilterPanelSpec, JSONValue, LocalizedText, OverlayWidth, PageActionSpec, PageBodyLayout, RecordDetailSpec, RecordMasterSpec, RecordTreeSpec, ResponsiveColumnCount, SizeableProps } from "@vastplan/ui-contract";
import { componentSizes, overlayWidths } from "@vastplan/ui-contract";
import { pageBodyLayouts } from "@vastplan/ui-contract";
import type { PluginExtensionAccess } from "@vastplan/plugin-extension-contract";
import type { WorkbenchFormDefinition } from "./form.js";
import { resolveFormWorkflowSurface, validateFormDefinition } from "./form.js";

export type { ActionSpec, CollectionSpec, CollectionCardSpec, CollectionCardValueFormat, CollectionCardFieldSpec, ComponentSize, DashboardBreakpoint, DashboardCompaction, DashboardGridItem, DashboardGridLayouts, DashboardGridSpec, DurationUnit, FilterPanelApplyMode, FilterPanelLayout, FilterPanelSpec, ColumnSpec, DataValueFormat, FilterSpec, FilterFieldKind, CollectionQueryMode, CollectionSelectionMode, CollectionView, FormActionPlacement, FormActionSpec, FormCondition, FormControlAlignment, FormDurationPresentation, FormFieldPresentation, FormLabelPlacement, FormLayout, FormPresentation, FormPresentationPreset, FormSchema, FormSectionPresentation, FormWidget, FormWorkflow, JSONValue, OverlayWidth, PageActionDisplay, PageActionOverflow, PageActionSpec, PageBodyLayout, RecordDetailSpec, RecordFieldSpec, RecordMasterSpec, RecordSectionSpec, RecordTreeSpec, ResponsiveColumnCount, SizeableProps } from "@vastplan/ui-contract";
export { durationUnits } from "@vastplan/ui-contract";
export { dashboardBreakpointOrder, dashboardDefaultBreakpoints, dashboardDefaultColumns, jsonSchemaDialect, message } from "@vastplan/ui-contract";
export { defineDashboardGrid } from "./dashboard.js";
export { resolveFormWorkflowSurface, validateFormPresentation } from "./form.js";
export type { WorkbenchFormActionContext, WorkbenchFormActionResult, WorkbenchFormAfterSubmitContext, WorkbenchFormBeforeSubmitResult, WorkbenchFormDefinition, WorkbenchFormFieldErrors, WorkbenchFormPreparation, WorkbenchFormSubmitContext, WorkbenchFormSubmitResult } from "./form.js";
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

export interface PageActionContext {
  action: PageActionSpec;
  refresh(): void;
}

export interface PageActionHostDefinition extends SizeableProps {
  id: string;
  actions: readonly PageActionSpec[];
  forms?: readonly WorkbenchFormDefinition[];
  overlays?: readonly WorkbenchOverlayDefinition[];
  runAction?(context: PageActionContext, signal: AbortSignal): Promise<CollectionActionResult | void>;
}

export interface CollectionSummaryMetric {
  id: string;
  label: LocalizedText;
  value: string | number;
  tone?: "neutral" | "info" | "success" | "warning" | "error";
  span?: ResponsiveColumnCount;
}

export interface CollectionSummary extends SizeableProps {
  title?: LocalizedText;
  /** 默认保留 Panel 兼容现有页面；plain 仅移除装饰性外框。 */
  appearance?: "panel" | "plain";
  columns?: ResponsiveColumnCount;
  metrics: readonly CollectionSummaryMetric[];
}

export type WorkbenchOverlayContent =
  | { kind: "json"; documents: readonly { title?: LocalizedText; value: JSONValue }[] }
  | { kind: "table"; columns: readonly ColumnSpec[]; rows: readonly Readonly<Record<string, unknown>>[]; rowKey?: string };

export interface WorkbenchOverlayDefinition<Row extends Record<string, unknown> = Record<string, unknown>> extends SizeableProps {
  id: string;
  surface: "dialog" | "drawer";
  title: LocalizedText;
  /** Dialog/Drawer 宽度，与内容组件 size 分离。 */
  width?: OverlayWidth;
  load(selected: readonly Row[], signal: AbortSignal): Promise<WorkbenchOverlayContent>;
}

/** Platform Profile policy for the collection presentation family. */
export interface WorkbenchPresentationConfig {
  collection?: { defaultDensity?: CollectionDensity; allowedDensities?: readonly CollectionDensity[] };
}

/** Owner-bound navigation intent. The trusted host resolves the referenced plugin catalog node. */
export interface WorkbenchPageNavigation {
  id: string;
  label: LocalizedText;
  parentMenuRef: { pluginID: string; nodeID: string };
  /** Trusted host validates this browser-visible managed service selector. */
  managementServiceID?: string;
  order?: number;
}

export interface CollectionPageDefinition<Row extends Record<string, unknown> = Record<string, unknown>> extends SizeableProps {
  id: string;
  path: string;
  title: LocalizedText;
  description?: LocalizedText;
  bodyLayout?: PageBodyLayout;
  /** Hides the page when the trusted session projection lacks a permission. */
  requiredPermissions?: readonly string[];
  /** At least one permission is sufficient to expose a shared governance page. */
  requiredAnyPermissions?: readonly string[];
  navigation?: WorkbenchPageNavigation;
  pageActions?: readonly PageActionSpec[];
  collection: CollectionSpec;
  load(query: CollectionQuery, signal: AbortSignal): Promise<CollectionResult<Row>>;
  loadSummary?(signal: AbortSignal): Promise<CollectionSummary>;
  forms?: readonly WorkbenchFormDefinition<Row>[];
  overlays?: readonly WorkbenchOverlayDefinition<Row>[];
  runAction?(context: CollectionActionContext<Row>, signal: AbortSignal): Promise<CollectionActionResult | void>;
  runPageAction?(context: PageActionContext, signal: AbortSignal): Promise<CollectionActionResult | void>;
}

/** A single routed page that composes several independently governed collections. */
export interface WorkspaceSectionDefinition extends SizeableProps {
  id: string;
  page: CollectionPageDefinition<any>;
}

export interface WorkspacePageDefinition extends SizeableProps {
  id: string;
  path: string;
  title: LocalizedText;
  description?: LocalizedText;
  bodyLayout?: PageBodyLayout;
  requiredPermissions?: readonly string[];
  requiredAnyPermissions?: readonly string[];
  navigation?: WorkbenchPageNavigation;
  sections: readonly WorkspaceSectionDefinition[];
}

export interface FormPageDefinition extends SizeableProps {
  id: string;
  path: string;
  title: LocalizedText;
  description?: LocalizedText;
  bodyLayout?: PageBodyLayout;
  requiredPermissions?: readonly string[];
  requiredAnyPermissions?: readonly string[];
  navigation?: WorkbenchPageNavigation;
  pageActions?: readonly PageActionSpec[];
  runPageAction?(context: PageActionContext, signal: AbortSignal): Promise<CollectionActionResult | void>;
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

interface RecordPageCommon<Row extends Record<string, unknown>> extends SizeableProps {
  id: string;
  path: string;
  title: LocalizedText;
  description?: LocalizedText;
  bodyLayout?: PageBodyLayout;
  requiredPermissions?: readonly string[];
  requiredAnyPermissions?: readonly string[];
  navigation?: WorkbenchPageNavigation;
  pageActions?: readonly PageActionSpec[];
  runPageAction?(context: PageActionContext, signal: AbortSignal): Promise<CollectionActionResult | void>;
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
  addWorkspacePage(page: WorkspacePageDefinition): void;
  addFormPage(page: FormPageDefinition): void;
  addRecordPage<Row extends Record<string, unknown>>(page: RecordPageDefinition<Row>): void;
}

export interface WorkbenchManagementCapability { capability: string; read?: readonly string[]; write?: readonly string[]; }
export interface WorkbenchManagementAPI { id: string; contractId: string; contractVersion: string; contractDigest: string; }
export interface WorkbenchManagedResource { kind: "service-unit"; kernel: "backend"; deployment: string; unitId: string; }
export interface WorkbenchManagementService { id: string; label?: string; logicalService: string; routingDomain: string; resource?: WorkbenchManagedResource; capabilities: readonly WorkbenchManagementCapability[]; apis?: readonly WorkbenchManagementAPI[]; }
export interface WorkbenchPortalRuntime { revision: number; id: string; tenantId: string; route: string; experience?: { permissions: readonly string[] }; management: { services: readonly WorkbenchManagementService[] }; }
export interface WorkbenchFrontendPluginContext extends WorkbenchPluginContext {
  readonly portal: Readonly<WorkbenchPortalRuntime>;
  readonly lifecycle: Readonly<{ pluginID: string; generation: string; signal: AbortSignal; reason: "bootstrap" | "replace" | "shutdown" }>;
  readonly i18n: Readonly<{ message(key: string, fallback: string, values?: import("@vastplan/ui-contract").MessageValues): import("@vastplan/ui-contract").MessageDescriptor }>;
  readonly extensions: PluginExtensionAccess;
}

export function managementServicesFor(portal: Readonly<WorkbenchPortalRuntime>, capability: string): readonly WorkbenchManagementService[] {
  return portal.management.services.filter((service) => service.capabilities.some((grant) => grant.capability === capability));
}

/** Makes page definitions discoverable and prevents a future arbitrary component escape hatch. */
export function defineCollectionPage<Row extends Record<string, unknown>>(definition: CollectionPageDefinition<Row>): CollectionPageDefinition<Row> {
	validateComponentSize(definition.size, `Collection page ${definition.id}`);
	validatePageBodyLayout(definition.bodyLayout, `Collection page ${definition.id}`);
	validatePermissionRequirements(definition.requiredPermissions, "Collection page requiredPermissions");
	validatePermissionRequirements(definition.requiredAnyPermissions, "Collection page requiredAnyPermissions");
  if (definition.collection.view === "cards" && definition.collection.query.mode !== "cursor") {
    throw new Error("Card Collection 必须使用 cursor 查询");
  }
  if (definition.collection.view === "cards" && definition.collection.card === undefined) {
    throw new Error("Card Collection 必须声明 card 呈现契约");
  }
  if (definition.collection.table?.virtualization !== undefined && !["auto", "always", "off"].includes(definition.collection.table.virtualization)) {
    throw new Error(`Collection ${definition.collection.id} 的表格虚拟化策略无效`);
  }
  if (definition.collection.view !== "table" && definition.collection.table !== undefined) {
    throw new Error(`Collection ${definition.collection.id} 只有 Table 视图可声明表格策略`);
  }
  validateFilterPanel(definition.collection.filterPanel, `Collection ${definition.collection.id}`);
  validateComponentSize(definition.collection.size, `Collection ${definition.collection.id}`);
  validateComponentSize(definition.collection.card?.size, `Collection ${definition.collection.id} card`);
  const forms = new Map((definition.forms ?? []).map((form) => [form.id, form]));
  if (forms.size !== (definition.forms ?? []).length) throw new Error("Collection 表单 ID 必须唯一");
  const overlays = new Map((definition.overlays ?? []).map((overlay) => [overlay.id, overlay]));
  if (overlays.size !== (definition.overlays ?? []).length) throw new Error("Collection Overlay ID 必须唯一");
  for (const form of forms.values()) validateFormDefinition(form);
  for (const overlay of overlays.values()) validateOverlayDefinition(overlay);
  const actions = definition.collection.actions ?? [];
  if (new Set(actions.map((action) => action.id)).size !== actions.length) throw new Error(`Collection ${definition.collection.id} 的 Action ID 必须唯一`);
  for (const action of actions) {
    validateAction(action, definition.runAction !== undefined);
    if (!["collection.toolbar", "collection.bulk", "record.row", "card.footer"].includes(action.placement)) throw new Error(`Collection ${definition.collection.id} 的 Action 位置无效: ${action.id}`);
    if (action.form !== undefined && !forms.has(action.form)) throw new Error(`Action ${action.id} 引用了未声明的表单 ${action.form}`);
    if (action.overlay !== undefined && !overlays.has(action.overlay)) throw new Error(`Action ${action.id} 引用了未声明的 Overlay ${action.overlay}`);
    if (action.form !== undefined && action.overlay !== undefined) throw new Error(`Action ${action.id} 不能同时打开表单和 Overlay`);
  }
  validatePageActions(definition.pageActions, forms, overlays, definition.runPageAction !== undefined, definition.id);
  return Object.freeze({ ...definition, collection: Object.freeze({ ...definition.collection }) });
}

export function defineWorkspacePage(definition: WorkspacePageDefinition): WorkspacePageDefinition {
  validateComponentSize(definition.size, `Workspace page ${definition.id}`);
  validatePageBodyLayout(definition.bodyLayout, `Workspace page ${definition.id}`);
  validatePermissionRequirements(definition.requiredPermissions, "Workspace page requiredPermissions");
  validatePermissionRequirements(definition.requiredAnyPermissions, "Workspace page requiredAnyPermissions");
  if (!validIdentifier(definition.id) || definition.sections.length === 0) throw new Error(`Workspace 页面定义无效: ${definition.id}`);
  const sectionIDs = definition.sections.map((section) => section.id);
  if (new Set(sectionIDs).size !== sectionIDs.length || sectionIDs.some((id) => !validIdentifier(id))) throw new Error(`Workspace ${definition.id} 的 Section ID 无效或重复`);
  const sections = definition.sections.map((section) => {
    validateComponentSize(section.size, `Workspace ${definition.id} section ${section.id}`);
    return Object.freeze({ ...section, page: defineCollectionPage(section.page) });
  });
  const pageActions = sections.flatMap((section) => section.page.pageActions ?? []);
  if (new Set(pageActions.map((action) => action.id)).size !== pageActions.length) throw new Error(`Workspace ${definition.id} 的 Page Action ID 必须跨 Section 唯一`);
  const referencedForms = pageActions.flatMap((action) => action.form === undefined ? [] : [action.form]);
  const referencedOverlays = pageActions.flatMap((action) => action.overlay === undefined ? [] : [action.overlay]);
  if (new Set(referencedForms).size !== referencedForms.length || new Set(referencedOverlays).size !== referencedOverlays.length) throw new Error(`Workspace ${definition.id} 的页面工作流 ID 必须跨 Section 唯一`);
  return Object.freeze({ ...definition, sections: Object.freeze(sections) });
}

export function defineFormPage(definition: FormPageDefinition): FormPageDefinition {
	validateComponentSize(definition.size, `Form page ${definition.id}`);
	validatePageBodyLayout(definition.bodyLayout, `Form page ${definition.id}`);
	validatePermissionRequirements(definition.requiredPermissions, "Form page requiredPermissions");
	validatePermissionRequirements(definition.requiredAnyPermissions, "Form page requiredAnyPermissions");
  if (definition.form.workflow.surface !== "page") throw new Error("Form Page 的 workflow.surface 必须为 page");
  validateFormDefinition(definition.form);
  validatePageActions(definition.pageActions, new Map(), new Map(), definition.runPageAction !== undefined, definition.id);
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
  validateComponentSize(definition.size, `Record page ${definition.id}`);
  validatePageBodyLayout(definition.bodyLayout, `Record page ${definition.id}`);
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
  for (const overlay of overlays.values()) validateOverlayDefinition(overlay);
  const actions = definition.actions ?? [];
  if (new Set(actions.map((action) => action.id)).size !== actions.length) throw new Error(`Record page ${definition.id} 的 Action ID 必须唯一`);
  for (const action of actions) {
    validateAction(action, definition.runAction !== undefined);
    if (!validIdentifier(action.id) || action.placement !== "record.detail") throw new Error(`Record page ${definition.id} 的 Action 位置无效: ${action.id}`);
    if (action.form !== undefined && !forms.has(action.form)) throw new Error(`Action ${action.id} 引用了未声明的表单 ${action.form}`);
    if (action.overlay !== undefined && !overlays.has(action.overlay)) throw new Error(`Action ${action.id} 引用了未声明的 Overlay ${action.overlay}`);
    if (action.form !== undefined && action.overlay !== undefined) throw new Error(`Action ${action.id} 不能同时打开表单和 Overlay`);
  }
  validatePageActions(definition.pageActions, forms, overlays, definition.runPageAction !== undefined, definition.id);
}

function validatePageBodyLayout(value: PageBodyLayout | undefined, label: string): void {
  if (value !== undefined && !pageBodyLayouts.includes(value)) throw new Error(`${label} 的 bodyLayout 无效: ${String(value)}`);
}

function validatePageActions(
  actions: readonly PageActionSpec[] | undefined,
  forms: ReadonlyMap<string, WorkbenchFormDefinition>,
  overlays: ReadonlyMap<string, WorkbenchOverlayDefinition>,
  hasRunAction: boolean,
  pageID: string,
): void {
  if (actions === undefined) return;
  if (new Set(actions.map((action) => action.id)).size !== actions.length) throw new Error(`Page ${pageID} 的页面 Action ID 必须唯一`);
  const allowedKeys = new Set(["id", "label", "icon", "tone", "display", "overflow", "order", "confirm", "form", "overlay", "requiredPermissions"]);
  for (const action of actions) {
    const unknownKey = Object.keys(action).find((key) => !allowedKeys.has(key));
    if (unknownKey !== undefined) throw new Error(`Page Action ${action.id} 包含不受支持的字段: ${unknownKey}`);
    validatePermissionRequirements(action.requiredPermissions, `Page Action ${action.id} requiredPermissions`);
    if (!validIdentifier(action.id) || action.icon === undefined || action.order !== undefined && (!Number.isSafeInteger(action.order) || Math.abs(action.order) > 1_000_000)) throw new Error(`Page Action 无效: ${action.id}`);
    if (action.display !== undefined && !["icon", "icon-label", "label"].includes(action.display)) throw new Error(`Page Action ${action.id} display 无效`);
    if (action.overflow !== undefined && !["auto", "always", "never"].includes(action.overflow)) throw new Error(`Page Action ${action.id} overflow 无效`);
    if (action.form !== undefined && !forms.has(action.form)) throw new Error(`Page Action ${action.id} 引用了未声明的表单 ${action.form}`);
    if (action.overlay !== undefined && !overlays.has(action.overlay)) throw new Error(`Page Action ${action.id} 引用了未声明的 Overlay ${action.overlay}`);
    if (action.form !== undefined && action.overlay !== undefined) throw new Error(`Page Action ${action.id} 不能同时打开表单和 Overlay`);
    if (action.form === undefined && action.overlay === undefined && !hasRunAction) throw new Error(`Page Action ${action.id} 必须由页面 runPageAction 工作流处理`);
  }
}

function validateAction(action: ActionSpec, hasRunAction: boolean): void {
  validatePermissionRequirements(action.requiredPermissions, `Action ${action.id} requiredPermissions`);
  if (!validIdentifier(action.id)) throw new Error(`Action ID 无效: ${action.id}`);
  if (action.icon === undefined) throw new Error(`Action ${action.id} 必须声明语义图标`);
  if (action.form === undefined && action.overlay === undefined && !hasRunAction) throw new Error(`Action ${action.id} 必须由页面 runAction 工作流处理`);
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
  validateFilterPanel(master.filterPanel, `MasterDetail ${master.id}`);
}

function validateFilterPanel(panel: FilterPanelSpec | undefined, owner: string): void {
  if (panel === undefined) return;
  validateComponentSize(panel.size, `${owner} FilterPanel`);
  if (panel.fields.length === 0 || panel.apply?.actionsPlacement !== undefined && panel.apply.actionsPlacement !== "last-cell") {
    throw new Error(`${owner} 的 FilterPanel 定义无效`);
  }
  const ids = panel.fields.map((field) => field.id);
  if (new Set(ids).size !== ids.length || ids.some((id) => !validFieldKey(id))) {
    throw new Error(`${owner} 的 FilterPanel 字段 ID 无效或重复`);
  }
}

function validIdentifier(value: string): boolean { return /^[A-Za-z0-9][A-Za-z0-9._-]{0,159}$/.test(value); }
function validFieldKey(value: string): boolean { return /^[A-Za-z0-9][A-Za-z0-9._-]{0,159}$/.test(value); }
function validSelectionParam(value: string): boolean { return /^[a-z][a-z0-9_-]{0,39}$/.test(value); }

function validateOverlayDefinition(overlay: WorkbenchOverlayDefinition): void {
  validateComponentSize(overlay.size, `Overlay ${overlay.id}`);
  validateOverlayWidth(overlay.width, `Overlay ${overlay.id} width`);
}

function validateComponentSize(value: ComponentSize | undefined, label: string): void {
  if (value !== undefined && !componentSizes.includes(value)) throw new Error(`${label} 的 size 无效: ${String(value)}`);
}

function validateOverlayWidth(value: OverlayWidth | undefined, label: string): void {
  if (value !== undefined && !overlayWidths.includes(value)) throw new Error(`${label} 无效: ${String(value)}`);
}
