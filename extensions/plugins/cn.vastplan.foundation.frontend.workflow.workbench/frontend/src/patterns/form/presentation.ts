import type { FormCondition, FormPresentation, FormSchema, FormWidget } from "@vastplan/ui-contract";
import { resolveFormPresentation } from "@vastplan/ui-primitives";

/** Workbench policy: every business form is inline unless it explicitly opts into another label placement. */
export function resolveWorkbenchFormPresentation(source: FormPresentation | undefined): FormPresentation {
  return resolveFormPresentation({ ...source, labelPlacement: source?.labelPlacement ?? "inline" });
}

export function evaluateFormCondition(condition: FormCondition, value: Readonly<Record<string, unknown>>, context: Readonly<Record<string, unknown>> = {}): boolean {
  if ("all" in condition) return condition.all.every((item) => evaluateFormCondition(item, value, context));
  if ("any" in condition) return condition.any.some((item) => evaluateFormCondition(item, value, context));
  if ("not" in condition) return !evaluateFormCondition(condition.not, value, context);
  const target = condition.pointer.startsWith("/context/") ? pointer(context, condition.pointer.slice("/context".length)) : pointer(value, condition.pointer);
  if ("exists" in condition) return condition.exists === target.found;
  if (!target.found) return false;
  if ("equals" in condition) return target.value === condition.equals;
  return condition.in.includes(target.value as never);
}

export function projectFormPresentation(schema: FormSchema, presentation: FormPresentation | undefined, value: Readonly<Record<string, unknown>>, context: Readonly<Record<string, unknown>>, text: (value: FormPresentationText) => string): FormSchema {
  const renderedSchema = withoutSectionOwnedObjectTitles(schema.schema, presentation);
  if (presentation?.fields === undefined || presentation.fields.length === 0) return renderedSchema === schema.schema ? schema : { ...schema, schema: renderedSchema };
  const uiSchema = clone(schema.uiSchema ?? {});
  for (const field of presentation.fields) {
    const node = uiNode(uiSchema, field.pointer);
    if (field.visibleWhen !== undefined && !evaluateFormCondition(field.visibleWhen, value, context)) node["ui:widget"] = "hidden";
    else if (field.widget !== undefined) node["ui:widget"] = widget(field.widget);
    if (field.readOnlyWhen !== undefined && evaluateFormCondition(field.readOnlyWhen, value, context)) node["ui:readonly"] = true;
    if (field.help !== undefined) node["ui:help"] = text(field.help);
    if (field.span !== undefined) {
      const options = record(node["ui:options"]);
      node["ui:options"] = { ...options, vastplanSpan: field.span };
    }
  }
  return { ...schema, schema: renderedSchema, uiSchema };
}

type FormPresentationText = NonNullable<NonNullable<FormPresentation["fields"]>[number]["help"]>;

function pointer(root: Readonly<Record<string, unknown>>, path: string): { found: boolean; value?: unknown } {
  if (path === "") return { found: true, value: root };
  if (!path.startsWith("/")) return { found: false };
  let value: unknown = root;
  for (const raw of path.slice(1).split("/")) {
    const key = raw.replace(/~1/g, "/").replace(/~0/g, "~");
    if (typeof value !== "object" || value === null || !Object.prototype.hasOwnProperty.call(value, key)) return { found: false };
    value = (value as Record<string, unknown>)[key];
  }
  return { found: true, value };
}

function uiNode(root: Record<string, unknown>, pointer: string): Record<string, unknown> {
  const parts = pointer.startsWith("/") ? pointer.slice(1).split("/").map((part) => part.replace(/~1/g, "/").replace(/~0/g, "~")) : [];
  let node = root;
  for (const part of parts) {
    const current = record(node[part]);
    node[part] = current;
    node = current;
  }
  return node;
}

/**
 * A sequential FormDialog section is the visible group label. A direct object
 * field in that section must therefore not render a second JSON Schema title.
 * This creates a display-only schema projection: validation constraints and
 * the source definition stay intact, while every Renderer receives one stable
 * title-ownership rule instead of plugin-specific ui:title workarounds.
 */
function withoutSectionOwnedObjectTitles(schema: FormSchema["schema"], presentation: FormPresentation | undefined): FormSchema["schema"] {
  if (presentation?.navigation !== "sections" || presentation.sections === undefined) return schema;
  const properties = record(schema.properties);
  const owned = new Set(presentation.sections.flatMap((section) => section.fields.map(directRootField).filter((field): field is string => field !== undefined)));
  let changed = false;
  const projected = { ...properties };
  for (const field of owned) {
    const property = record(properties[field]);
    if (property.type !== "object" || !("title" in property)) continue;
    const { title: _title, ...withoutTitle } = property;
    projected[field] = withoutTitle;
    changed = true;
  }
  return changed ? { ...schema, properties: projected } as FormSchema["schema"] : schema;
}

function directRootField(pointer: string): string | undefined {
  if (!pointer.startsWith("/")) return undefined;
  const parts = pointer.slice(1).split("/");
  if (parts.length !== 1 || parts[0] === "") return undefined;
  return parts[0]!.replace(/~1/g, "/").replace(/~0/g, "~");
}

function widget(value: FormWidget): string {
  return ({ text: "text", textarea: "textarea", number: "updown", select: "select", boolean: "checkbox", date: "date", datetime: "alt-datetime", credentialRef: "secretRef", secretMaterial: "password", hidden: "hidden" } as const)[value];
}

function record(value: unknown): Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value) ? { ...value as Record<string, unknown> } : {};
}

function clone<T>(value: T): T {
  return JSON.parse(JSON.stringify(value)) as T;
}
