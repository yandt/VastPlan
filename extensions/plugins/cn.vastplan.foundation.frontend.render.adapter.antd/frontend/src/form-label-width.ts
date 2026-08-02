import { componentSizeRecipes, type ComponentSize } from "@vastplan/ui-primitives";

/** Resolve one stable inline-label column width for a complete dynamic form. */
export function resolveFormLabelWidth(schema: unknown, size: ComponentSize): number {
  const minimum = componentSizeRecipes.formDialog[size].inlineLabelMinWidth;
  return Math.max(minimum, ...formLabels(schema).map((label) => estimateLabelWidth(label, componentSizeRecipes.control[size].fontSize)));
}

function formLabels(schema: unknown): string[] {
  if (!isRecord(schema)) return [];
  const children = isRecord(schema.properties) ? Object.values(schema.properties).flatMap(formLabels) : [];
  return schema.type === "boolean" || typeof schema.title !== "string" ? children : [schema.title, ...children];
}

function estimateLabelWidth(label: string, fontSize: number): number {
  const em = Array.from(label).reduce((total, character) => total + characterWidth(character), 0);
  return Math.ceil(em * fontSize + 12);
}

function characterWidth(character: string): number {
  if (/\s/u.test(character)) return 0.3;
  if (/^[\u0000-\u007f]$/u.test(character)) return /[A-Z0-9]/u.test(character) ? 0.68 : 0.56;
  return 1;
}

function isRecord(value: unknown): value is Readonly<Record<string, unknown>> { return typeof value === "object" && value !== null && !Array.isArray(value); }
