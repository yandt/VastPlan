import type { LocalizedText } from "@vastplan/ui-contract";

export function localizeFormFieldErrors(errors: Readonly<Record<string, LocalizedText>>, text: (value: LocalizedText) => string): Readonly<Record<string, string>> {
  return Object.freeze(Object.fromEntries(Object.entries(errors).map(([path, value]) => [formFieldErrorPath(path), text(value)])));
}

export function formFieldErrorPath(path: string): string {
  if (path === "$form" || !path.startsWith("/")) return path;
  return path.slice(1).split("/").map((part) => part.replace(/~1/g, "/").replace(/~0/g, "~")).join(".");
}
