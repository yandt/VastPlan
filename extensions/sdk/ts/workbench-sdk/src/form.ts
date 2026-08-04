import type {
  ComponentSize,
  FormActionSpec,
  FormPresentation,
  FormSchema,
  FormWorkflow,
  JSONValue,
  LocalizedText,
  OverlayWidth,
  SizeableProps,
} from "@vastplan/ui-contract";
import {
  componentSizes,
  durationUnits,
  formControlAlignments,
  overlayWidths,
  semanticIconNames,
} from "@vastplan/ui-contract";

export interface WorkbenchFormSubmitContext<Row extends Record<string, unknown> = Record<string, unknown>> {
  value: Readonly<Record<string, unknown>>;
  selected: readonly Row[];
  context?: Readonly<Record<string, unknown>>;
}

export interface WorkbenchFormSubmitResult {
  fieldErrors?: WorkbenchFormFieldErrors;
  data?: JSONValue;
}

export interface WorkbenchFormBeforeSubmitResult {
  cancelled?: boolean;
  value?: Readonly<Record<string, unknown>>;
  fieldErrors?: WorkbenchFormFieldErrors;
}

export interface WorkbenchFormAfterSubmitContext<Row extends Record<string, unknown> = Record<string, unknown>> extends WorkbenchFormSubmitContext<Row> {
  result?: WorkbenchFormSubmitResult;
}

export interface WorkbenchFormActionContext<Row extends Record<string, unknown> = Record<string, unknown>> extends WorkbenchFormSubmitContext<Row> {
  action: FormActionSpec;
}

export interface WorkbenchFormActionResult {
  notify?: { title: LocalizedText; content?: LocalizedText; kind?: "success" | "info" | "warning" | "error" };
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

export interface WorkbenchFormDefinition<Row extends Record<string, unknown> = Record<string, unknown>> extends SizeableProps {
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
  runAction?(context: WorkbenchFormActionContext<Row>, signal: AbortSignal): Promise<WorkbenchFormActionResult | void>;
  beforeSubmit?(context: WorkbenchFormSubmitContext<Row>, signal: AbortSignal): Promise<WorkbenchFormBeforeSubmitResult | void>;
  submit(context: WorkbenchFormSubmitContext<Row>, signal: AbortSignal): Promise<WorkbenchFormSubmitResult | void>;
  afterSubmit?(context: WorkbenchFormAfterSubmitContext<Row>, signal: AbortSignal): Promise<void>;
}

export function resolveFormWorkflowSurface(workflow: FormWorkflow): "page" | "dialog" {
  const surface = workflow.surface ?? "dialog";
  if (surface !== "page" && surface !== "dialog") throw new Error(`表单工作流 surface 不受支持: ${String(surface)}`);
  return surface;
}

export function validateFormDefinition(form: WorkbenchFormDefinition): void {
  validateComponentSize(form.size, `表单 ${form.id}`);
  validateComponentSize(form.workflow.size, `表单 ${form.id} workflow`);
  validateOverlayWidth(form.workflow.dialogWidth, `表单 ${form.id} dialogWidth`);
  const surface = resolveFormWorkflowSurface(form.workflow);
  const dialogHeight = form.workflow.dialogHeight;
  if (dialogHeight !== undefined && (!Number.isSafeInteger(dialogHeight) || dialogHeight < 160 || dialogHeight > 10_000)) {
    throw new Error(`表单 ${form.id} 的 dialogHeight 必须是 160..10000 之间的整数像素值`);
  }
  if (surface === "page" && dialogHeight !== undefined) throw new Error(`表单 ${form.id} 的 dialogHeight 仅可用于 dialog surface`);
  const actions = form.workflow.actions ?? [];
  if (new Set(actions.map((action) => action.id)).size !== actions.length) throw new Error(`表单 ${form.id} 的 Action ID 必须唯一`);
  const allowedActionKeys = new Set(["id", "label", "icon", "placement", "tone", "requiresValid", "confirm"]);
  const semanticIcons = new Set<string>(semanticIconNames);
  for (const action of actions) {
    const unknownKey = Object.keys(action).find((key) => !allowedActionKeys.has(key));
    if (unknownKey !== undefined) throw new Error(`表单 ${form.id} 的 Action ${action.id} 包含不受支持的字段: ${unknownKey}`);
    if (!validIdentifier(action.id) || action.label === undefined || !semanticIcons.has(action.icon) || !["footer.start", "footer.end"].includes(action.placement) ||
        action.tone !== undefined && !["neutral", "primary", "danger"].includes(action.tone) || action.requiresValid !== undefined && typeof action.requiresValid !== "boolean") {
      throw new Error(`表单 ${form.id} 的 Action ${action.id} 无效`);
    }
  }
  if (actions.length > 0 && form.runAction === undefined) throw new Error(`表单 ${form.id} 声明了 Action 但没有 runAction 工作流`);
  validateFormPresentation(form.presentation, form.id);
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
    if (field.widget === "duration") {
      const node = schemaNode(form.schema.schema, field.pointer);
      if (node?.type !== "integer" && node?.type !== "number") throw new Error(`表单 ${form.id} 的 duration 字段必须是 number 或 integer`);
    }
  }
}

export function validateFormPresentation(presentation: FormPresentation | undefined, formID = "dynamic-form"): void {
  if (presentation === undefined) return;
  validateComponentSize(presentation.size, `表单 ${formID} presentation`);
  if (presentation.preset !== undefined && !["compact", "standard", "comfortable", "guided"].includes(presentation.preset)) throw new Error(`表单 ${formID} 的 preset 无效`);
  if (presentation.labelPlacement !== undefined && !["inline", "stacked", "inside-inline"].includes(presentation.labelPlacement)) throw new Error(`表单 ${formID} 的 labelPlacement 无效`);
  if (presentation.controlAlignment !== undefined && !formControlAlignments.includes(presentation.controlAlignment)) throw new Error(`表单 ${formID} 的 controlAlignment 无效`);
  if (presentation.navigation !== undefined && !["sections", "tabs", "steps"].includes(presentation.navigation)) throw new Error(`表单 ${formID} 的 navigation 无效`);
  validateFormColumns(formID, presentation.columns, presentation.columnWidths, "presentation");
  const sections = presentation.sections ?? [];
  if (new Set(sections.map((section) => section.id)).size !== sections.length) throw new Error(`表单 ${formID} 的 section ID 必须唯一`);
  for (const section of sections) {
    if (!validIdentifier(section.id) || section.fields.length === 0 || section.fields.some((field) => !field.startsWith("/"))) throw new Error(`表单 ${formID} 的 section ${section.id} 无效`);
    validateFormColumns(formID, section.columns, section.columnWidths, `section ${section.id}`);
  }
  for (const field of presentation.fields ?? []) validateDurationPresentation(field, formID);
}

function validateDurationPresentation(field: NonNullable<FormPresentation["fields"]>[number], formID: string): void {
  if (field.widget !== "duration") {
    if (field.duration !== undefined) throw new Error(`表单 ${formID} 只有 duration widget 可以声明单位配置`);
    return;
  }
  const config = field.duration;
  const allowed = new Set(durationUnits);
  if (config === undefined || !allowed.has(config.storageUnit) || config.units.length === 0 || config.units.length > durationUnits.length ||
      new Set(config.units).size !== config.units.length || config.units.some((unit) => !allowed.has(unit)) ||
      config.defaultUnit !== undefined && !config.units.includes(config.defaultUnit)) {
    throw new Error(`表单 ${formID} 的 duration 单位配置无效`);
  }
}

function validateComponentSize(value: ComponentSize | undefined, label: string): void {
  if (value !== undefined && !componentSizes.includes(value)) throw new Error(`${label} 的 size 无效: ${String(value)}`);
}

function validateOverlayWidth(value: OverlayWidth | undefined, label: string): void {
  if (value !== undefined && !overlayWidths.includes(value)) throw new Error(`${label} 无效: ${String(value)}`);
}

function validIdentifier(value: string): boolean {
  return /^[A-Za-z0-9][A-Za-z0-9._-]{0,159}$/.test(value);
}

function validateFormColumns(formID: string, columns: number | undefined, widths: readonly number[] | undefined, owner: string): void {
  const count = columns ?? 1;
  if (!Number.isSafeInteger(count) || count < 1 || count > 4) throw new Error(`表单 ${formID} 的 ${owner}.columns 必须在 1..4`);
  if (widths === undefined) return;
  const total = widths.reduce((sum, value) => sum + value, 0);
  if (widths.length !== count || widths.some((value) => !Number.isFinite(value) || value <= 0 || value > 100) || Math.abs(total - 100) > 0.01) {
    throw new Error(`表单 ${formID} 的 ${owner}.columnWidths 数量必须匹配列数、每项大于 0 且合计 100`);
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
