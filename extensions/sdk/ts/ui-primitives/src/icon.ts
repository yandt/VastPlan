import { semanticIconGlyph } from "@vastplan/icon-catalog/semantic";
import type { CSSProperties, ReactElement } from "react";
import type { SemanticIconName, SizeableProps } from "@vastplan/ui-contract";
import { renderIconGlyph } from "./icon-svg.js";
import { useComponentSize } from "./component-size.js";

export { semanticIconNames } from "@vastplan/ui-contract";
export type { SemanticIconName } from "@vastplan/ui-contract";

export interface VastPlanIconProps extends SizeableProps {
  name: SemanticIconName;
  label?: string;
  className?: string;
  style?: CSSProperties;
}

/** Framework-neutral, VastPlan-owned SVG renderer for the semantic icon contract. */
export function VastPlanIcon({ name, label, size: requestedSize, className, style }: VastPlanIconProps): ReactElement {
  return renderIconGlyph(semanticIconGlyph(name), { name, label, size: useComponentSize(requestedSize), className, style });
}
