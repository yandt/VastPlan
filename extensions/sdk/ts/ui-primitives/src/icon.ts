import { semanticIconGlyph } from "@vastplan/icon-catalog/semantic";
import type { CSSProperties, ReactElement } from "react";
import type { SemanticIconName } from "@vastplan/ui-contract";
import { renderIconGlyph } from "./icon-svg.js";

export { semanticIconNames } from "@vastplan/ui-contract";
export type { SemanticIconName } from "@vastplan/ui-contract";

export interface VastPlanIconProps {
  name: SemanticIconName;
  label?: string;
  size?: "sm" | "md" | "lg";
  className?: string;
  style?: CSSProperties;
}

/** Framework-neutral, VastPlan-owned SVG renderer for the semantic icon contract. */
export function VastPlanIcon({ name, label, size = "md", className, style }: VastPlanIconProps): ReactElement {
  return renderIconGlyph(semanticIconGlyph(name), { name, label, size, className, style });
}
