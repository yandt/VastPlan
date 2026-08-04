import type { CSSProperties } from "react";

/** The shared viewport guard for every Portal dialog, independent of its renderer. */
export const dialogViewportMaximum = "90vh";
export const formDialogViewportMaximum = "95vh";
const dialogChromeReserve = 144;

/**
 * Resolves an optional governed pixel height without allowing a Dialog to escape
 * the current viewport. Omitted height preserves content-driven sizing.
 */
export function dialogFrameStyle(height?: number, viewportMaximum = dialogViewportMaximum): CSSProperties {
  return height === undefined
    ? { maxHeight: viewportMaximum }
    : { height: `min(${height}px, ${viewportMaximum})`, maxHeight: viewportMaximum };
}

/** Keeps scroll ownership inside the Dialog body, never on the page behind it. */
export function dialogBodyStyle(contentOverflow: "visible" | "scroll", viewportMaximum = dialogViewportMaximum): CSSProperties {
  if (contentOverflow === "visible") return { minHeight: 0 };
  return { minHeight: 0, maxHeight: `calc(${viewportMaximum} - ${dialogChromeReserve}px)`, overflowY: "auto" };
}
