import type { ReactElement } from "react";
import type { ComponentSize } from "@vastplan/ui-contract";
import { VastPlanIcon } from "./icon.js";
import { renderIconGlyph } from "./icon-svg.js";
import type { PortalNavigationIconSpec, PortalNavigationIconState } from "./portal-runtime.js";

export function NavigationIcon({ icon, state = "normal", label, size = "md" }: {
  icon: PortalNavigationIconSpec;
  state?: PortalNavigationIconState;
  label?: string;
  size?: ComponentSize;
}): ReactElement {
  if (icon.kind === "semantic") return <VastPlanIcon name={icon.name} label={label} size={size} />;
  const resolvedState = icon.states[state] ?? icon.states.normal;
  return <span data-vastplan-navigation-icon data-motion={state === "active" || state === "loading" ? icon.motion : "none"}>
    {renderIconGlyph(resolvedState, { name: `${icon.pluginID}/${icon.name}`, label, size })}
  </span>;
}
