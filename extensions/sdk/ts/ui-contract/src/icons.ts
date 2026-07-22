/**
 * Stable semantic icon vocabulary shared by functional plugins, Workbench and
 * every render adapter. Names describe intent, never a framework component.
 */
export const semanticIconNames = Object.freeze([
  "add",
  "remove",
  "edit",
  "search",
  "settings",
  "success",
  "warning",
  "error",
  "info",
  "close",
  "menu",
  "import",
  "export",
  "publish",
  "refresh",
  "columns",
  "copy",
  "download",
  "upload",
  "more",
] as const);

export type SemanticIconName = (typeof semanticIconNames)[number];
