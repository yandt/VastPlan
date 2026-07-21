import type { FormCondition, FormPresentation, FormSchema, FormWidget } from "@vastplan/ui-contract";

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
  if (presentation?.fields === undefined || presentation.fields.length === 0) return schema;
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
  return { ...schema, uiSchema };
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

function widget(value: FormWidget): string {
  return ({ text: "text", textarea: "textarea", number: "updown", select: "select", boolean: "checkbox", date: "date", datetime: "alt-datetime", credentialRef: "secretRef", secretMaterial: "password", hidden: "hidden" } as const)[value];
}

function record(value: unknown): Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value) ? { ...value as Record<string, unknown> } : {};
}

function clone<T>(value: T): T {
  return JSON.parse(JSON.stringify(value)) as T;
}
