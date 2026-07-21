import type { FormPresentation } from "@vastplan/ui-contract";

export function secretMaterialPointers(presentation: FormPresentation | undefined): readonly string[] {
  return presentation?.fields?.filter((field) => field.widget === "secretMaterial").map((field) => field.pointer) ?? [];
}

export function containsSecretMaterial(value: Readonly<Record<string, unknown>>, pointers: readonly string[]): boolean {
  return pointers.some((pointer) => {
    const result = readPointer(value, pointer);
    return result.found && result.value !== undefined && result.value !== null && result.value !== "";
  });
}

/** Clone without retaining one-time material in baselines or closed Workbench state. */
export function discardSecretMaterial(value: Readonly<Record<string, unknown>>, pointers: readonly string[]): Record<string, unknown> {
  return cloneWithoutMaterial(value, pointers.map(parts)) as Record<string, unknown>;
}

function readPointer(root: Readonly<Record<string, unknown>>, pointer: string): { found: boolean; value?: unknown } {
  let value: unknown = root;
  for (const key of parts(pointer)) {
    if (typeof value !== "object" || value === null || Array.isArray(value) || !Object.prototype.hasOwnProperty.call(value, key)) return { found: false };
    value = (value as Record<string, unknown>)[key];
  }
  return { found: true, value };
}

function parts(pointer: string): string[] {
  return pointer.startsWith("/") ? pointer.slice(1).split("/").map((value) => value.replace(/~1/g, "/").replace(/~0/g, "~")) : [];
}

function cloneWithoutMaterial(value: unknown, candidates: readonly (readonly string[])[]): unknown {
  if (Array.isArray(value)) return value.map((item, index) => cloneWithoutMaterial(item, descend(candidates, String(index))));
  if (typeof value !== "object" || value === null) return value;
  const result: Record<string, unknown> = {};
  for (const [key, item] of Object.entries(value)) {
    const next = descend(candidates, key);
    if (next.some((path) => path.length === 0)) continue;
    result[key] = cloneWithoutMaterial(item, next);
  }
  return result;
}

function descend(candidates: readonly (readonly string[])[], key: string): readonly (readonly string[])[] {
  return candidates.flatMap((path) => path[0] === key ? [path.slice(1)] : []);
}
