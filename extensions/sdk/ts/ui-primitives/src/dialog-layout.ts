import type { CSSProperties } from "react";

/** The shared viewport guard for every Portal dialog, independent of its renderer. */
export const dialogViewportMaximum = "90vh";
const dialogChromeReserve = 144;

/**
 * Resolves an optional governed pixel height without allowing a Dialog to escape
 * the current viewport. Omitted height preserves content-driven sizing.
 */
export function dialogFrameStyle(height?: number): CSSProperties {
  return height === undefined
    ? { maxHeight: dialogViewportMaximum }
    : { height: `min(${height}px, ${dialogViewportMaximum})`, maxHeight: dialogViewportMaximum };
}

/** Keeps scroll ownership inside the Dialog body, never on the page behind it. */
export function dialogBodyStyle(contentOverflow: "visible" | "scroll"): CSSProperties {
  if (contentOverflow === "visible") return { minHeight: 0 };
  return { minHeight: 0, maxHeight: `calc(${dialogViewportMaximum} - ${dialogChromeReserve}px)`, overflowY: "auto" };
}
