import type { AuthorizationPermissionSelector } from "@vastplan/platform-admin";

export function normalizePermissionSelectorInputs(values: unknown): AuthorizationPermissionSelector[] {
  if (!Array.isArray(values)) return [];
  const unique = new Set<string>();
  for (const value of values) {
    if (typeof value !== "string") continue;
    const normalized = value.trim();
    if (normalized !== "") unique.add(normalized);
  }
  return [...unique].map((value) => ({ kind: value.includes("*") ? "glob" : "exact", value }));
}
