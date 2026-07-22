import { createElement } from "react";
import type { CSSProperties, ReactElement, ReactNode } from "react";
import type { SemanticIconName } from "@vastplan/ui-contract";

export { semanticIconNames } from "@vastplan/ui-contract";
export type { SemanticIconName } from "@vastplan/ui-contract";

export interface VastPlanIconProps {
  name: SemanticIconName;
  label?: string;
  size?: "sm" | "md" | "lg";
  className?: string;
  style?: CSSProperties;
}

const pixels = { sm: 16, md: 20, lg: 24 } as const;

/** Framework-neutral, VastPlan-owned SVG renderer for the semantic icon contract. */
export function VastPlanIcon({ name, label, size = "md", className, style }: VastPlanIconProps): ReactElement {
  return createElement("svg", {
    "data-vastplan-icon": name,
    viewBox: "0 0 24 24",
    width: pixels[size],
    height: pixels[size],
    fill: "none",
    stroke: "currentColor",
    strokeWidth: 1.8,
    strokeLinecap: "round",
    strokeLinejoin: "round",
    className,
    style: { display: "inline-block", flex: "0 0 auto", verticalAlign: "middle", ...style },
    focusable: "false",
    role: label === undefined ? undefined : "img",
    "aria-label": label,
    "aria-hidden": label === undefined ? true : undefined,
  }, glyph(name));
}

const path = (key: string, d: string) => createElement("path", { key, d });
const circle = (key: string, cx: number, cy: number, r: number, filled = false) => createElement("circle", { key, cx, cy, r, ...(filled ? { fill: "currentColor", stroke: "none" } : {}) });
const rect = (key: string, x: number, y: number, width: number, height: number, rx: number) => createElement("rect", { key, x, y, width, height, rx });

function glyph(name: SemanticIconName): ReactNode {
  switch (name) {
    case "add": return [path("a", "M12 5v14"), path("b", "M5 12h14")];
    case "remove": return path("a", "M5 12h14");
    case "edit": return [path("a", "m4 20 4.2-1 10-10a2.1 2.1 0 0 0-3-3l-10 10L4 20Z"), path("b", "m13.8 7.4 3 3")];
    case "search": return [circle("a", 11, 11, 6), path("b", "m16 16 4 4")];
    case "settings": return [circle("a", 12, 12, 3), path("b", "M19.4 15a1.7 1.7 0 0 0 .3 1.9l.1.1-2.8 2.8-.1-.1a1.7 1.7 0 0 0-1.9-.3 1.7 1.7 0 0 0-1 1.6v.2h-4V21a1.7 1.7 0 0 0-1-1.6 1.7 1.7 0 0 0-1.9.3l-.1.1L4.2 17l.1-.1a1.7 1.7 0 0 0 .3-1.9A1.7 1.7 0 0 0 3 14H2.8v-4H3a1.7 1.7 0 0 0 1.6-1 1.7 1.7 0 0 0-.3-1.9L4.2 7 7 4.2l.1.1A1.7 1.7 0 0 0 9 4.6 1.7 1.7 0 0 0 10 3v-.2h4V3a1.7 1.7 0 0 0 1 1.6 1.7 1.7 0 0 0 1.9-.3l.1-.1L19.8 7l-.1.1a1.7 1.7 0 0 0-.3 1.9 1.7 1.7 0 0 0 1.6 1h.2v4H21a1.7 1.7 0 0 0-1.6 1Z")];
    case "success": return [circle("a", 12, 12, 9), path("b", "m8 12 2.6 2.6L16.5 9")];
    case "warning": return [path("a", "M10.3 4.2 2.7 18a2 2 0 0 0 1.8 3h15a2 2 0 0 0 1.8-3L13.7 4.2a2 2 0 0 0-3.4 0Z"), path("b", "M12 9v4"), path("c", "M12 17h.01")];
    case "error": return [circle("a", 12, 12, 9), path("b", "m9 9 6 6"), path("c", "m15 9-6 6")];
    case "info": return [circle("a", 12, 12, 9), path("b", "M12 11v6"), path("c", "M12 7h.01")];
    case "close": return [path("a", "m6 6 12 12"), path("b", "m18 6-12 12")];
    case "menu": return [path("a", "M4 7h16"), path("b", "M4 12h16"), path("c", "M4 17h16")];
    case "import": return [path("a", "M12 3v12"), path("b", "m7 10 5 5 5-5"), path("c", "M5 21h14")];
    case "export": return [path("a", "M12 15V3"), path("b", "m7 8 5-5 5 5"), path("c", "M5 21h14")];
    case "publish": return [path("a", "M12 16V4"), path("b", "m7 9 5-5 5 5"), path("c", "M5 14v6h14v-6")];
    case "refresh": return [path("a", "M20 7v5h-5"), path("b", "M4 17v-5h5"), path("c", "M6.1 8.2A7 7 0 0 1 18.8 9L20 12"), path("d", "m4 12 1.2 3A7 7 0 0 0 17.9 15.8")];
    case "columns": return [rect("a", 3, 4, 18, 16, 1), path("b", "M9 4v16"), path("c", "M15 4v16")];
    case "copy": return [rect("a", 8, 8, 12, 12, 2), path("b", "M16 8V6a2 2 0 0 0-2-2H6a2 2 0 0 0-2 2v8a2 2 0 0 0 2 2h2")];
    case "download": return [path("a", "M12 3v12"), path("b", "m7 10 5 5 5-5"), path("c", "M5 21h14")];
    case "upload": return [path("a", "M12 15V3"), path("b", "m7 8 5-5 5 5"), path("c", "M5 21h14")];
    case "more": return [circle("a", 5, 12, 1, true), circle("b", 12, 12, 1, true), circle("c", 19, 12, 1, true)];
  }
}
